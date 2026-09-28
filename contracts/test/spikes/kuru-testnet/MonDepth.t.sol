// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;
import "forge-std/Test.sol";
import {ILedger, LEDGER, USDC} from "./KuruTestnet.t.sol";

address constant MONUSDC = 0xfdbE356828c8f5A5d5ed4f69ddE0816f4058Ef61;

contract Seller {
    receive() external payable {}
    function id() public view returns (uint40) { return uint40(ILedger(LEDGER).userRegistry(address(this))); }
    function sellAll(uint256 monWei) external {
        ILedger(LEDGER).deposit{value: monWei}(address(0), monWei);
        uint256[6][] memory o = new uint256[6][](1);
        o[0] = [uint256(1), monWei / 1e10, 1, 1, 0, 0]; // IOC sell, size 1e8/MON, px 0.000001
        (bool ok, bytes memory r) = MONUSDC.call(abi.encodeWithSelector(0x6947f147, id(), o, new uint256[](0), uint256(1)));
        require(ok, string(r));
    }
}

contract MonDepthTest is Test {
    function test_sweepBids() public {
        vm.createSelectFork("https://testnet-rpc.monad.xyz");
        Seller s = new Seller();
        uint256 amt = 10_000_000 ether;
        vm.deal(address(s), amt);
        s.sellAll(amt);
        uint256 usdc = ILedger(LEDGER).getBalance(address(s), USDC);
        uint256 monLeft = ILedger(LEDGER).getBalance(address(s), address(0));
        emit log_named_decimal_uint("USDC received", usdc, 6);
        emit log_named_decimal_uint("MON sold", amt - monLeft, 18);
    }
}
