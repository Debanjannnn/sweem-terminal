// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;
import "forge-std/Test.sol";

// Kuru testnet "v2" (unpublished ABI, reverse-engineered from on-chain txs 0x3b185a82.., 0x43f9fbf2..)
interface IERC20 { function approve(address, uint256) external returns (bool); function balanceOf(address) external view returns (uint256); }
interface ILedger { // 0x6384e9b2...
    function deposit(address token, uint256 amt) external payable; // auto-registers account
    function userRegistry(address) external view returns (uint256);
    function getBalance(address user, address token) external view returns (uint256);
}
interface IMarket {
    // 0x6947f147: order = (side 0=buy/1=sell, size, price, tif 0=GTC/1=IOC, 0, 0)
    function placeOrders(uint40 acct, uint256[6][] calldata orders, uint256[] calldata cancels, uint256 clientId) external;
    // 0x9d49a2c5: (acct, isBuy, amount, limitPrice, deadline)
    function marketOrder(uint40 acct, bool isBuy, uint128 amt, uint128 limitPx, uint64 deadline) external;
    function bestBidAsk() external view returns (uint256, uint256);
}

address constant LEDGER = 0x6384e9b2Bf3b65e1535403a0A543b5FDA905eE22;
address constant WETHUSDC = 0xa9C2936656a7D2143720BcD91Ba8506200B7CbE7;
address constant USDC = 0xEe0722ead54f1B4fe97bE399Be43BC0226a6f97E;
address constant WETH = 0x8B6C5fafeF85B030bB1e71ae7ac085cC2380aAf8;

contract Acct {
    function dep(address t, uint256 a) external { IERC20(t).approve(LEDGER, a); ILedger(LEDGER).deposit(t, a); }
    function id() public view returns (uint40) { return uint40(ILedger(LEDGER).userRegistry(address(this))); }
    function order(uint256 side, uint256 size, uint256 px, uint256 tif) external {
        uint256[6][] memory o = new uint256[6][](1);
        o[0] = [side, size, px, tif, 0, 0];
        (bool ok, bytes memory r) = WETHUSDC.call(abi.encodeWithSelector(0x6947f147, id(), o, new uint256[](0), uint256(1)));
        require(ok, string(r));
    }
    function mkt(bool isBuy, uint128 amt, uint128 px) external {
        (bool ok, bytes memory r) = WETHUSDC.call(abi.encodeWithSelector(0x9d49a2c5, id(), isBuy, amt, px, uint64(block.timestamp + 60)));
        require(ok, string(r));
    }
}

contract KuruTestnetTest is Test {
    Acct maker; Acct taker;
    function setUp() public {
        vm.createSelectFork("https://testnet-rpc.monad.xyz");
        maker = new Acct(); taker = new Acct();
        deal(WETH, address(maker), 1e18); deal(USDC, address(taker), 5000e6);
        maker.dep(WETH, 1e18); taker.dep(USDC, 5000e6);
        maker.order(1, 5e9, 300000, 0); // GTC sell 0.5 WETH @ 3000.00
        (, uint256 ask) = IMarket(WETHUSDC).bestBidAsk();
        assertEq(ask, 300000, "maker ask not resting");
    }
    function _log(string memory tag) internal {
        emit log_named_uint(string.concat(tag, " taker WETH"), ILedger(LEDGER).getBalance(address(taker), WETH));
        emit log_named_uint(string.concat(tag, " taker USDC"), ILedger(LEDGER).getBalance(address(taker), USDC));
        emit log_named_uint(string.concat(tag, " maker USDC"), ILedger(LEDGER).getBalance(address(maker), USDC));
    }
    function test_iocBuyFillsMaker() public {
        taker.order(0, 1e9, 300000, 1); // IOC buy 0.1 WETH @ 3000
        _log("ioc");
        assertEq(ILedger(LEDGER).getBalance(address(taker), WETH), 1e17);
        assertGt(ILedger(LEDGER).getBalance(address(maker), USDC), 0);
    }
    function test_marketBuyFillsMaker() public {
        taker.mkt(true, 300e6, 310000); // amt semantics unverified: try quote 300 USDC (6dp)
        _log("mkt");
        assertGt(ILedger(LEDGER).getBalance(address(taker), WETH), 0);
    }
}
