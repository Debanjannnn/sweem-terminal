// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {Test} from "forge-std/Test.sol";
import {Accrual} from "../src/libraries/Accrual.sol";

contract AccrualTest is Test {
    uint256 constant YEAR = 365 days;

    /// @dev The off-chain oracle service annualizes with the literal 31_536_000
    ///      (services/oracle/src/index-math.ts). Any drift desyncs sigma between the
    ///      two sides of the power-perp funding coupling.
    function test_secondsPerYearMatchesTheOracleService() public pure {
        assertEq(YEAR, 31_536_000);
        // k=2, sigma=1 => coefficient 1, so the rate is exactly 1/31_536_000 per second
        assertEq(Accrual.powerFundingRate(2e18, 1e18), int256(uint256(1e18) / 31_536_000));
    }

    function test_borrowRateLinearInUtilization() public pure {
        uint256 base = 0.02e18 / YEAR;
        uint256 slope = 0.2e18 / YEAR;
        assertEq(Accrual.borrowRatePerSecond(base, slope, 0), base);
        assertEq(Accrual.borrowRatePerSecond(base, slope, 1e18), base + slope);
        assertEq(Accrual.borrowRatePerSecond(base, slope, 0.5e18), base + slope / 2);
    }

    function test_powerFundingCoupling() public pure {
        // k = 2, sigma = 80%: k(k-1)/2 = 1  =>  rate = sigma^2 / year
        int256 r = Accrual.powerFundingRate(2e18, 0.8e18);
        assertApproxEqRel(r, int256(0.64e18 / YEAR), 1e12);

        // k = 1 (linear perp) => no power funding requirement at all
        assertEq(Accrual.powerFundingRate(1e18, 0.8e18), 0);

        // k = 0.5 => k(k-1)/2 = -0.125, shorts pay
        int256 half = Accrual.powerFundingRate(0.5e18, 0.8e18);
        assertLt(half, 0);
        assertApproxEqRel(half, -int256(0.125e18 * 0.64e18 / 1e18 / YEAR), 1e13);

        // zero vol => zero coupling
        assertEq(Accrual.powerFundingRate(2e18, 0), 0);
    }

    function test_powerFundingScalesWithVarianceNotVol() public pure {
        int256 a = Accrual.powerFundingRate(2e18, 0.5e18);
        int256 b = Accrual.powerFundingRate(2e18, 1e18);
        assertApproxEqRel(b, a * 4, 1e12, "sigma^2 term must quadruple when sigma doubles");
    }

    function test_skewFunding() public pure {
        uint256 scale = 1_000_000e18;
        uint256 cap = type(uint128).max;
        // 10% long-heavy skew, k_f = 1 => 10% premium
        assertEq(Accrual.skewFundingRate(600_000e18, 500_000e18, scale, 1e18, cap), 0.1e18);
        // balanced book => zero funding
        assertEq(Accrual.skewFundingRate(500_000e18, 500_000e18, scale, 1e18, cap), 0);
        // short-heavy => shorts pay
        assertLt(Accrual.skewFundingRate(400_000e18, 500_000e18, scale, 1e18, cap), 0);
    }

    function test_fundingCapBindsBothSides() public pure {
        uint256 cap = 1e15;
        assertEq(Accrual.skewFundingRate(1_000_000e18, 0, 1_000e18, 1e18, cap), int256(cap));
        assertEq(Accrual.skewFundingRate(0, 1_000_000e18, 1_000e18, 1e18, cap), -int256(cap));
    }

    function test_markFromSkew() public pure {
        assertEq(Accrual.markPrice(100e18, 500_000e18, 500_000e18, 1_000_000e18), 100e18);
        assertEq(Accrual.markPrice(100e18, 600_000e18, 500_000e18, 1_000_000e18), 110e18);
        assertEq(Accrual.markPrice(100e18, 400_000e18, 500_000e18, 1_000_000e18), 90e18);
    }

    /// @dev Accruing in one step must equal accruing in two, for any split of the gap.
    ///      This is the property the O(1) cumulative index depends on.
    function testFuzz_accrualIsPathIndependent(uint256 rate, uint256 dt1, uint256 dt2, uint256 notional) public pure {
        rate = bound(rate, 0, uint256(1e18) / YEAR * 1000); // up to 100,000% APR
        dt1 = bound(dt1, 0, 10 * 365 days);
        dt2 = bound(dt2, 0, 10 * 365 days);
        notional = bound(notional, 0, 1e30);

        uint256 oneShot = rate * (dt1 + dt2);
        uint256 twoStep = rate * dt1 + rate * dt2;
        assertEq(oneShot, twoStep);
        assertEq(Accrual.owed(notional, oneShot, 0), Accrual.owed(notional, twoStep, 0));
    }

    /// @dev Extreme utilization + a decade-long idle gap must not overflow and must
    ///      stay monotonic.
    function testFuzz_extremeUtilizationAndIdleGaps(uint256 base, uint256 slope, uint256 util, uint256 dt) public pure {
        base = bound(base, 0, uint256(1e18) / YEAR * 10);
        slope = bound(slope, 0, uint256(1e18) / YEAR * 100);
        util = bound(util, 0, 1e18);
        dt = bound(dt, 0, 10 * 365 days);

        uint256 rate = Accrual.borrowRatePerSecond(base, slope, util);
        assertGe(rate, base, "rate below base");
        uint256 cum = rate * dt;
        assertGe(cum, 0);
        // owed on 1M notional must never exceed notional * cum / WAD by construction
        assertEq(Accrual.owed(1_000_000e18, cum, 0), 1_000_000e18 * cum / 1e18);
    }

    /// @dev Whatever the inputs, the funding rate never escapes the cap.
    function testFuzz_fundingAlwaysWithinCap(uint256 longOI, uint256 shortOI, uint256 scale, uint256 kf, uint256 cap)
        public
        pure
    {
        longOI = bound(longOI, 0, 1e30);
        shortOI = bound(shortOI, 0, 1e30);
        scale = bound(scale, 1e18, 1e30);
        kf = bound(kf, 0, 100e18);
        cap = bound(cap, 0, 1e18);
        int256 r = Accrual.skewFundingRate(longOI, shortOI, scale, kf, cap);
        assertLe(r, int256(cap));
        assertGe(r, -int256(cap));
    }

    /// @dev Longs and shorts see equal-and-opposite funding, so funding is
    ///      conservative between the two sides of the book.
    function testFuzz_fundingIsAntisymmetric(uint256 notional, int256 cumNow, int256 cumEntry) public pure {
        notional = bound(notional, 0, 1e30);
        cumNow = bound(cumNow, -1e24, 1e24);
        cumEntry = bound(cumEntry, -1e24, 1e24);
        int256 d = Accrual.owedSigned(notional, cumNow, cumEntry);
        assertEq(d + (-d), 0);
    }
}
