// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import "forge-std/Test.sol";
import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {SweemBasketFactory} from "../../src/SweemBasketFactory.sol";
import {SweemBasket, Component} from "../../src/SweemBasket.sol";
import {IAccountCore} from "../../src/interfaces/IAccountCore.sol";
import {IKuruOrderBook} from "../../src/interfaces/IKuruOrderBook.sol";
import {KuruMath} from "../../src/libraries/KuruMath.sol";

IAccountCore constant AC = IAccountCore(0x6384e9b2Bf3b65e1535403a0A543b5FDA905eE22);
address constant USDC = 0xEe0722ead54f1B4fe97bE399Be43BC0226a6f97E;
address constant MON_USDC = 0xfdbE356828c8f5A5d5ed4f69ddE0816f4058Ef61;
address constant WETH_USDC = 0xa9C2936656a7D2143720BcD91Ba8506200B7CbE7;
address constant CBBTC_USDC = 0x5BDEA6F9F9abA34F4EcB9B865646A792b835ef7f;
address constant XAUT_USDC = 0x0B4dD2A7b09d5c5401149fFe51301Cc589017343;

struct NativeOrder {
    uint8 side;
    uint96 quantity;
    uint32 price;
    uint8 tif;
    uint8 executionInstruction;
    uint32 minSizeAfterBlock;
}

interface IBook {
    function batch(uint40 userId, NativeOrder[] calldata orders, uint8[] calldata cancels, bytes32 clientOrderId) external;
    function cancelAllOrders(uint40 userId) external;
}

/// Quotes a GTC bid + ask on one market from its own AccountCore account.
contract Maker {
    receive() external payable {}

    function fund(address base, uint256 baseAmt, uint256 usdcAmt) external {
        if (base == address(0)) {
            AC.deposit{value: baseAmt}(address(0), baseAmt);
        } else {
            IERC20(base).approve(address(AC), baseAmt);
            AC.deposit(base, baseAmt);
        }
        IERC20(USDC).approve(address(AC), usdcAmt);
        AC.deposit(USDC, usdcAmt);
    }

    function quote(address market, uint96 qty, uint32 bid, uint32 ask) external {
        uint40 id = AC.userRegistry(address(this));
        IBook(market).cancelAllOrders(id);
        NativeOrder[] memory o = new NativeOrder[](2);
        o[0] = NativeOrder(0, qty, bid, 0, 0, 0);
        o[1] = NativeOrder(1, qty, ask, 0, 0, 0);
        IBook(market).batch(id, o, new uint8[](0), bytes32(0));
    }
}

contract SweemBasketForkTest is Test {
    SweemBasketFactory factory;
    SweemBasket blue;
    mapping(address => Maker) makers;
    address alice = makeAddr("alice");
    address bob = makeAddr("bob");

    function setUp() public {
        vm.createSelectFork("https://testnet-rpc.monad.xyz");
        factory = new SweemBasketFactory(address(AC), USDC, address(this));
        factory.setMarketAllowed(MON_USDC, true);
        factory.setMarketAllowed(WETH_USDC, true);
        factory.setMarketAllowed(CBBTC_USDC, true);
        Component[] memory c = new Component[](3);
        c[0] = Component(MON_USDC, 4000);
        c[1] = Component(WETH_USDC, 3000);
        c[2] = Component(CBBTC_USDC, 3000);
        blue = SweemBasket(factory.createBasket("sweem Blue", "BLUE", c));

        _seed(MON_USDC, 1_000_000 ether, 1e14, 25_000); // 1M MON @ 0.025
        _seed(WETH_USDC, 100 ether, 1e12, 275_000); // 100 WETH @ 2750.00
        _seed(CBBTC_USDC, 5e8, 5e8, 8_415_000); // 5 cbBTC @ 84150.00

        for (uint256 i; i < 2; ++i) {
            address u = i == 0 ? alice : bob;
            deal(USDC, u, 10_000e6);
            vm.prank(u);
            IERC20(USDC).approve(address(blue), type(uint256).max);
        }
    }

    function _seed(address market, uint256 baseAmt, uint96 qty, uint32 center) internal {
        Maker m = new Maker();
        makers[market] = m;
        address base = IKuruOrderBook(market).baseToken();
        if (base == address(0)) vm.deal(address(m), baseAmt);
        else deal(base, address(m), baseAmt);
        deal(USDC, address(m), 1_000_000e6);
        m.fund(base, baseAmt, 1_000_000e6);
        _quote(market, qty, center);
    }

    function _quote(address market, uint96 qty, uint32 center) internal {
        makers[market].quote(market, qty, uint32(uint256(center) * 999 / 1000), uint32(uint256(center) * 1001 / 1000));
    }

    function _nav() internal view returns (uint256 v) {
        uint256[] memory h = blue.holdings();
        Component[] memory c = blue.components();
        for (uint256 i; i < h.length; ++i) v += KuruMath.valueAtMid(IKuruOrderBook(c[i].market), h[i]);
    }

    function _redeemAll(address u, uint256 sh) internal returns (uint256) {
        vm.prank(u);
        return blue.redeem(sh, 0, block.timestamp + 60);
    }

    function test_fullLifecycle() public {
        vm.prank(alice);
        uint256 a = blue.mint(1000e6, 990e18, block.timestamp + 60);
        uint256 aliceSpent = 10_000e6 - IERC20(USDC).balanceOf(alice);
        assertEq(a, aliceSpent * 1e12);
        assertApproxEqRel(aliceSpent, 1000e6, 0.001e18);
        assertGt(blue.accountId(), 0);
        uint256[] memory h = blue.holdings();
        for (uint256 i; i < h.length; ++i) assertGt(h[i], 0);
        assertApproxEqRel(_nav(), 1000e6, 0.005e18);

        _quote(WETH_USDC, 5e11, 302_500); // WETH +10%, smaller size: maker sold some inventory
        uint256 supply = blue.totalSupply();
        uint256 expected = supply * 500e6 / _nav();
        vm.prank(bob);
        uint256 b = blue.mint(500e6, expected * 99 / 100, block.timestamp + 60);
        assertApproxEqRel(b, expected, 0.005e18);

        uint256 out1 = _redeemAll(alice, a / 2);
        uint256 bobOut = _redeemAll(bob, b);
        uint256 out2 = _redeemAll(alice, a - a / 2);
        assertEq(blue.totalSupply(), 0);
        assertEq(AC.getBalance(address(blue), USDC), 0);
        // alice gains ~3% from WETH (30% weight) +10%; costs ~0.35% round trip
        assertGe(out1 + out2, 1020e6);
        assertGe(bobOut, 495e6);
        emit log_named_decimal_uint("alice out", out1 + out2, 6);
        emit log_named_decimal_uint("bob out", bobOut, 6);
    }

    function test_factoryAllowlistOnFork() public {
        address[] memory m = factory.allowedMarkets();
        assertEq(m.length, 3);
        assertTrue(factory.isBasket(address(blue)));
        assertEq(blue.tokens(0), address(0)); // native MON base

        Component[] memory c = new Component[](1);
        c[0] = Component(XAUT_USDC, 10_000);
        vm.expectRevert(SweemBasket.MarketNotAllowed.selector);
        factory.createBasket("gold", "AU", c);
        factory.setMarketAllowed(XAUT_USDC, true);
        address gold = factory.createBasket("gold", "AU", c);
        assertEq(factory.baskets()[1], gold);
    }
}
