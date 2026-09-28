// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import "forge-std/Test.sol";
import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";
import {SweemBasketFactory} from "../../src/SweemBasketFactory.sol";
import {SweemBasket, Component} from "../../src/SweemBasket.sol";
import {KuruMath} from "../../src/libraries/KuruMath.sol";
import {IKuruOrderBook} from "../../src/interfaces/IKuruOrderBook.sol";
import {MockERC20, MockAccountCore, MockOrderBook} from "./Mocks.sol";

contract Fixture is Test {
    MockERC20 usdc;
    MockERC20 weth;
    MockAccountCore ac;
    MockOrderBook monBook; // native base
    MockOrderBook wethBook;
    SweemBasketFactory factory;
    SweemBasket basket;
    address alice = makeAddr("alice");
    address bob = makeAddr("bob");

    function setUp() public virtual {
        usdc = new MockERC20("USDC", 6);
        weth = new MockERC20("WETH", 18);
        ac = new MockAccountCore();
        usdc.mint(address(ac), 1e30);
        monBook = new MockOrderBook(ac, address(0), address(usdc), 1e6, 1e8, 1e10);
        monBook.setPx(24_990, 25_010); // 0.025
        wethBook = new MockOrderBook(ac, address(weth), address(usdc), 100, 1e10, 1e8);
        wethBook.setPx(274_900, 275_100); // 2750
        factory = new SweemBasketFactory(address(ac), address(usdc), address(this));
        factory.setMarketAllowed(address(monBook), true);
        factory.setMarketAllowed(address(wethBook), true);
        basket = SweemBasket(factory.createBasket("sweem Test", "TST", _comps(address(monBook), 4000, address(wethBook), 6000)));
        for (uint256 i; i < 3; ++i) {
            address u = i == 0 ? alice : i == 1 ? bob : address(this);
            usdc.mint(u, 1e12);
            vm.prank(u);
            usdc.approve(address(basket), type(uint256).max);
        }
    }

    function _comps(address m0, uint16 w0, address m1, uint16 w1) internal pure returns (Component[] memory c) {
        c = new Component[](2);
        c[0] = Component(m0, w0);
        c[1] = Component(m1, w1);
    }

    function _mint(address u, uint256 amt) internal returns (uint256) {
        vm.prank(u);
        return basket.mint(amt, 0, block.timestamp);
    }

    function _redeem(address u, uint256 sh) internal returns (uint256) {
        vm.prank(u);
        return basket.redeem(sh, 0, block.timestamp);
    }

    function _nav() internal view returns (uint256 v) {
        uint256[] memory h = basket.holdings();
        v = KuruMath.valueAtMid(IKuruOrderBook(address(monBook)), h[0]) + KuruMath.valueAtMid(IKuruOrderBook(address(wethBook)), h[1]);
    }
}

contract SweemBasketTest is Fixture {
    function midExt(address m) external view returns (uint256) {
        return KuruMath.mid(IKuruOrderBook(m));
    }

    function test_firstMint_sharesAreSpentUsdcAt18dp_holdingsAtWeights() public {
        uint256 bal0 = usdc.balanceOf(alice);
        uint256 sh = _mint(alice, 1000e6);
        uint256 spent = bal0 - usdc.balanceOf(alice);
        assertEq(sh, spent * 1e12);
        assertApproxEqRel(spent, 1000e6, 0.001e18);
        uint256[] memory h = basket.holdings();
        assertApproxEqRel(KuruMath.valueAtMid(IKuruOrderBook(address(monBook)), h[0]), 400e6, 0.002e18);
        assertApproxEqRel(KuruMath.valueAtMid(IKuruOrderBook(address(wethBook)), h[1]), 600e6, 0.002e18);
        assertEq(basket.accountId(), 1);
    }

    function test_secondMint_samePrice_proportionalShares() public {
        uint256 a = _mint(alice, 1000e6);
        uint256 b = _mint(bob, 500e6);
        assertApproxEqRel(b, a / 2, 0.003e18);
    }

    function test_secondMint_afterPriceMove_splitsByHoldingsValue() public {
        _mint(alice, 1000e6);
        wethBook.setPx(302_390, 302_610); // WETH +10%
        uint256 supply = basket.totalSupply();
        uint256 navBefore = _nav();
        uint256[] memory h0 = basket.holdings();
        uint256 b = _mint(bob, 500e6);
        uint256[] memory h1 = basket.holdings();
        // each leg grows by the same fraction -> composition kept
        assertApproxEqRel((h1[0] - h0[0]) * 1e18 / h0[0], (h1[1] - h0[1]) * 1e18 / h0[1], 0.002e18);
        assertApproxEqRel(b, supply * 500e6 / navBefore, 0.003e18);
    }

    function test_redeem_partialThenFull() public {
        uint256 sh = _mint(alice, 1000e6);
        uint256 out1 = _redeem(alice, sh / 2);
        uint256 out2 = _redeem(alice, sh - sh / 2);
        assertEq(basket.totalSupply(), 0);
        assertApproxEqRel(out1 + out2, 1000e6, 0.003e18); // 2x taker fee + spread
        assertGe(out1 + out2, 996e6);
    }

    function test_dust_fullRedeemLeavesLessThanOneLot() public {
        _mint(alice, 1234_567891);
        _redeem(alice, basket.balanceOf(alice));
        uint256[] memory h = basket.holdings();
        assertLt(h[0], 1e10);
        assertLt(h[1], 1e8);
    }

    function test_mint_slippage() public {
        vm.prank(alice);
        vm.expectRevert(SweemBasket.Slippage.selector);
        basket.mint(1000e6, 1001e18, block.timestamp);
    }

    function test_redeem_slippage() public {
        uint256 sh = _mint(alice, 1000e6);
        vm.prank(alice);
        vm.expectRevert(SweemBasket.Slippage.selector);
        basket.redeem(sh, 1000e6, block.timestamp);
    }

    function test_deadline() public {
        vm.warp(1000);
        vm.startPrank(alice);
        vm.expectRevert(SweemBasket.Expired.selector);
        basket.mint(1e6, 0, 999);
        vm.expectRevert(SweemBasket.Expired.selector);
        basket.redeem(1, 0, 999);
        vm.stopPrank();
    }

    function test_zeroAmount() public {
        vm.startPrank(alice);
        vm.expectRevert(SweemBasket.ZeroAmount.selector);
        basket.mint(0, 0, block.timestamp);
        vm.expectRevert(SweemBasket.ZeroAmount.selector);
        basket.redeem(0, 0, block.timestamp);
        vm.stopPrank();
    }

    function test_pause_blocksMintNotRedeem() public {
        uint256 sh = _mint(alice, 100e6);
        factory.setPaused(true);
        vm.prank(alice);
        vm.expectRevert(SweemBasket.Paused.selector);
        basket.mint(100e6, 0, block.timestamp);
        assertGt(_redeem(alice, sh), 0);
    }

    function test_delistedMarket_blocksMintNotRedeem() public {
        uint256 sh = _mint(alice, 100e6);
        factory.setMarketAllowed(address(wethBook), false);
        vm.prank(alice);
        vm.expectRevert(SweemBasket.MarketNotAllowed.selector);
        basket.mint(100e6, 0, block.timestamp);
        assertGt(_redeem(alice, sh), 0);
    }

    function test_thinBook_firstMint_refundsUnfilled() public {
        wethBook.setFill(5000);
        uint256 bal0 = usdc.balanceOf(alice);
        uint256 sh = _mint(alice, 1000e6);
        uint256 spent = bal0 - usdc.balanceOf(alice);
        assertApproxEqRel(spent, 700e6, 0.001e18); // 400 MON + half of 600 WETH
        assertEq(sh, spent * 1e12);
        assertEq(ac.getBalance(address(basket), address(usdc)), 0);
    }

    function test_thinBook_secondMint_sharesFollowWorstLeg() public {
        uint256 a = _mint(alice, 1000e6);
        wethBook.setFill(5000);
        uint256 b = _mint(bob, 1000e6);
        assertApproxEqRel(b, a / 2, 0.003e18); // WETH leg only half filled
    }

    function test_emptyBook_secondMint_reverts() public {
        _mint(alice, 1000e6);
        wethBook.setPx(274_900, 0); // no asks, still priced by bid
        vm.prank(bob);
        vm.expectRevert(SweemBasket.ZeroAmount.selector);
        basket.mint(100e6, 0, block.timestamp);
    }

    function test_mid_sentinels() public {
        wethBook.setPx(type(uint32).max, 300);
        assertEq(this.midExt(address(wethBook)), 300);
        wethBook.setPx(200, 0);
        assertEq(this.midExt(address(wethBook)), 200);
        wethBook.setPx(type(uint32).max, 0);
        vm.expectRevert(KuruMath.NoPrice.selector);
        this.midExt(address(wethBook));
    }

    function test_toUsdc_units() public view {
        // 1 WETH @ 2750.00 ; 100 MON @ 0.025
        assertEq(KuruMath.toUsdc(IKuruOrderBook(address(wethBook)), 1e18, 275_000), 2750e6);
        assertEq(KuruMath.toUsdc(IKuruOrderBook(address(monBook)), 100e18, 25_000), 2.5e6);
        assertEq(KuruMath.floorToLot(IKuruOrderBook(address(monBook)), 12345e10 + 99), 12345e10);
    }
}

contract SweemBasketFactoryTest is Fixture {
    event MarketAllowed(address indexed market, bool allowed);

    function test_allowlist_ownerOnly_andEnumerates() public {
        vm.prank(alice);
        vm.expectRevert(abi.encodeWithSelector(Ownable.OwnableUnauthorizedAccount.selector, alice));
        factory.setMarketAllowed(address(1), true);
        vm.prank(alice);
        vm.expectRevert(abi.encodeWithSelector(Ownable.OwnableUnauthorizedAccount.selector, alice));
        factory.setPaused(true);

        vm.expectEmit(address(factory));
        emit MarketAllowed(address(wethBook), false);
        factory.setMarketAllowed(address(wethBook), false);
        address[] memory m = factory.allowedMarkets();
        assertEq(m.length, 1);
        assertEq(m[0], address(monBook));
    }

    function test_create_registers() public {
        assertTrue(factory.isBasket(address(basket)));
        assertEq(factory.baskets().length, 1);
        assertEq(basket.components().length, 2);
        assertEq(basket.tokens(1), address(weth));
        assertEq(basket.decimals(), 18);
    }

    function test_create_rejectsBadInput() public {
        vm.expectRevert(SweemBasket.BadWeights.selector);
        factory.createBasket("x", "x", _comps(address(monBook), 5000, address(wethBook), 4999));
        vm.expectRevert(SweemBasket.BadWeights.selector);
        factory.createBasket("x", "x", _comps(address(monBook), 10_000, address(wethBook), 0));
        vm.expectRevert(SweemBasket.BadWeights.selector);
        factory.createBasket("x", "x", new Component[](0));
        vm.expectRevert(SweemBasket.DuplicateMarket.selector);
        factory.createBasket("x", "x", _comps(address(monBook), 5000, address(monBook), 5000));

        MockOrderBook notAllowed = new MockOrderBook(ac, address(weth), address(usdc), 100, 1e10, 1e8);
        vm.expectRevert(SweemBasket.MarketNotAllowed.selector);
        factory.createBasket("x", "x", _comps(address(monBook), 5000, address(notAllowed), 5000));

        MockOrderBook wethQuoted = new MockOrderBook(ac, address(0), address(weth), 100, 1e10, 1e8);
        factory.setMarketAllowed(address(wethQuoted), true);
        vm.expectRevert(SweemBasket.MarketNotAllowed.selector);
        factory.createBasket("x", "x", _comps(address(monBook), 5000, address(wethQuoted), 5000));
    }
}
