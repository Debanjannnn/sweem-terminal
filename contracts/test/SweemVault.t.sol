// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {BaseTest} from "./Base.t.sol";
import {SweemVault} from "../src/SweemVault.sol";

contract SweemVaultTest is BaseTest {
    function test_depositMintsEighteenDecimalShares() public view {
        assertEq(vault.decimals(), 18);
        assertEq(vault.totalAssets(), 1_000_000e6);
        assertEq(vault.balanceOf(lp), 1_000_000e18);
    }

    function test_tvlCapCapsDeposits() public {
        assertEq(vault.maxDeposit(lp), 5_000_000e6 - 1_000_000e6);
        vm.prank(lp);
        vault.deposit(1_000_000e6, lp);
        assertEq(vault.maxDeposit(lp), 3_000_000e6);

        _fund(bob, 5_000_000e6);
        vm.prank(bob);
        vm.expectRevert();
        vault.deposit(4_000_000e6, bob);
    }

    function test_withdrawRoundTrip() public {
        uint256 before = susd.balanceOf(lp);
        uint256 shares = vault.balanceOf(lp);
        vm.prank(lp);
        vault.redeem(shares, lp, lp);
        assertEq(susd.balanceOf(lp), before + 1_000_000e6);
    }

    function test_utilizationAndBorrowRate() public {
        assertEq(vault.utilization(), 0);
        assertEq(vault.borrowRatePerSecond(), uint256(0.02e18) / YEAR);

        vm.prank(alice);
        engine.openPosition(VOL, 500_000e18, 200_000e6); // notional = 500k * 0.8 = 400k

        // 400k notional against ~1M TVL == 40% utilization
        assertApproxEqRel(vault.utilization(), 0.4e18, 0.02e18);
        assertGt(vault.borrowRatePerSecond(), uint256(0.02e18) / YEAR);
    }

    function test_utilizationCapBlocksOpening() public {
        // 1M notional against 1M TVL is 100% > 80% cap
        vm.prank(alice);
        vm.expectRevert(SweemVault.UtilizationCapExceeded.selector);
        engine.openPosition(VOL, 1_500_000e18, 600_000e6);
    }

    function test_lpsCannotWithdrawRentedCapital() public {
        vm.prank(alice);
        engine.openPosition(VOL, 500_000e18, 200_000e6); // 400k notional

        // 400k notional at an 80% cap reserves 500k of the 1M TVL
        assertApproxEqRel(vault.maxWithdraw(lp), 500_000e6, 0.05e18);

        vm.prank(lp);
        vm.expectRevert();
        vault.withdraw(900_000e6, lp, lp);
    }

    function test_onlyEngineMayPayOut() public {
        vm.prank(alice);
        vm.expectRevert();
        vault.payOut(alice, 1e18);
    }

    function test_pauseBlocksDepositsAndWithdrawals() public {
        vm.prank(admin);
        vault.pause();
        assertEq(vault.maxDeposit(lp), 0);
        assertEq(vault.maxWithdraw(lp), 0);
        vm.prank(lp);
        vm.expectRevert();
        vault.deposit(1e6, lp);
    }

    function test_decimalConversionIsExplicit() public view {
        assertEq(vault.toWad(1_000_000), 1e18); // 1 USDC -> 1e18
        assertEq(vault.toAsset(1e18), 1_000_000);
        assertEq(vault.toAsset(1e12 - 1), 0); // sub-unit dust truncates
    }

    function testFuzz_depositRedeemNeverMintsValue(uint256 amount) public {
        amount = bound(amount, 1e6, 1_000_000e6);
        _fund(bob, amount);
        uint256 before = susd.balanceOf(bob);
        vm.startPrank(bob);
        uint256 shares = vault.deposit(amount, bob);
        uint256 out = vault.redeem(shares, bob, bob);
        vm.stopPrank();
        assertLe(out, amount, "redeem returned more than deposited");
        assertEq(susd.balanceOf(bob), before - amount + out);
    }
}
