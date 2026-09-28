// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;
import "forge-std/Test.sol";
import {IERC20, IOrderBook} from "./KuruMarket.t.sol";

contract Buyer {
    receive() external payable {}
    function buy(address ob, uint96 q) external returns (uint256) {
        IERC20(0x754704Bc059F8C67012fEd69BC8A327a5aafb603).approve(ob, type(uint256).max);
        return IOrderBook(ob).placeAndExecuteMarketBuy(q, 0, false, false);
    }
}

contract DepthTest is Test {
    address constant USDC = 0x754704Bc059F8C67012fEd69BC8A327a5aafb603;
    function _run(string memory n, address ob, address base, uint256 pp, uint256 usd) internal {
        uint256 snap = vm.snapshotState();
        Buyer b = new Buyer();
        deal(USDC, address(b), usd * 1e6);
        uint256 got = base == address(0) ? address(b).balance : IERC20(base).balanceOf(address(b));
        try b.buy(ob, uint96(usd * pp)) {} catch { emit log(string.concat(n, " REVERT")); }
        got = (base == address(0) ? address(b).balance : IERC20(base).balanceOf(address(b))) - got;
        emit log_named_uint(string.concat(n, " usd=", vm.toString(usd), " base raw out"), got);
        vm.revertToState(snap);
    }
    function test_depth() public {
        vm.createSelectFork("https://rpc.monad.xyz");
        uint256[3] memory sizes = [uint256(100), 1000, 10000];
        for (uint i; i < 3; i++) {
            _run("MON",   0x065C9d28E428A0db40191a54d33d5b7c71a9C394, address(0), 1e8, sizes[i]);
            _run("WETH",  0xa6aFD386135B7D41A6C40C525abC4A1019b0D132, 0xEE8c0E9f1BFFb4Eb878d8f15f368A02a35481242, 1e4, sizes[i]);
            _run("cbBTC", 0x40c49F171202F91ff5d2faE34c22dD2BFdD22aF0, 0xd18B7EC58Cdf4876f6AFebd3Ed1730e4Ce10414b, 1e4, sizes[i]);
            _run("XAUt0", 0x851145eaeFdc37956B08dA829fa31722199F3f07, 0x01bFF41798a0BcF287b996046Ca68b395DbC1071, 1e4, sizes[i]);
        }
    }
}
