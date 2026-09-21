// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {SD59x18, sd} from "@prb/math/SD59x18.sol";

/// @notice Cumulative-index borrow + funding accrual math. O(1) — no per-position loops.
/// @dev Rates are per-second, 1e18 fixed point. Cumulative indices are per-unit-notional
///      integrals of those rates, also 1e18.
library Accrual {
    uint256 internal constant WAD = 1e18;
    int256 internal constant SWAD = 1e18;
    /// @dev Must stay exactly 31_536_000 — services/oracle/src/index-math.ts annualizes
    ///      with the same literal, and any drift desyncs sigma between the two.
    uint256 internal constant SECONDS_PER_YEAR = 31_536_000;

    /// @param utilization 1e18 (1e18 == 100%)
    function borrowRatePerSecond(uint256 baseRate, uint256 slope, uint256 utilization) internal pure returns (uint256) {
        return baseRate + (slope * utilization) / WAD;
    }

    function owed(uint256 notional, uint256 cumNow, uint256 cumEntry) internal pure returns (uint256) {
        return (notional * (cumNow - cumEntry)) / WAD;
    }

    function owedSigned(uint256 notional, int256 cumNow, int256 cumEntry) internal pure returns (int256) {
        return (int256(notional) * (cumNow - cumEntry)) / SWAD;
    }

    /// @dev Mark is derived from vault skew, not an order book:
    ///      mark = index * (1 + skew / skewScale)  =>  (mark - index)/index = skew / skewScale.
    /// @param skewScale notional at which the mark premium is 100%, 1e18
    /// @param cap absolute per-second cap, 1e18
    /// @return rate per-second funding rate, positive = longs pay
    function skewFundingRate(uint256 longOI, uint256 shortOI, uint256 skewScale, uint256 fundingK, uint256 cap)
        internal
        pure
        returns (int256 rate)
    {
        if (skewScale == 0) return 0;
        int256 skew = int256(longOI) - int256(shortOI);
        int256 premium = (skew * SWAD) / int256(skewScale); // (mark - index)/index, 1e18
        rate = (int256(fundingK) * premium) / SWAD;
        rate = clamp(rate, cap);
    }

    function markPrice(uint256 index, uint256 longOI, uint256 shortOI, uint256 skewScale)
        internal
        pure
        returns (uint256)
    {
        if (skewScale == 0) return index;
        int256 premium = ((int256(longOI) - int256(shortOI)) * SWAD) / int256(skewScale);
        int256 mark = (int256(index) * (SWAD + premium)) / SWAD;
        return mark < 0 ? 0 : uint256(mark);
    }

    /// @dev Power-perp funding coupling: a perp on P^k requires k(k-1)/2 * sigma^2 per
    ///      unit time. `sigma` is the annualized volatility index — the very index the
    ///      volatility perp trades. Returned per-second, positive = longs pay.
    /// @param sigma annualized volatility, 1e18 (e.g. 0.8e18 == 80%)
    function powerFundingRate(int256 k, uint256 sigma) internal pure returns (int256) {
        if (k == 0 || sigma == 0) return 0;
        SD59x18 kk = sd(k);
        SD59x18 coeff = kk.mul(kk.sub(sd(SWAD))).div(sd(2 * SWAD)); // k(k-1)/2
        SD59x18 varAnnual = sd(int256(sigma)).mul(sd(int256(sigma))); // sigma^2 per year
        return coeff.mul(varAnnual).unwrap() / int256(SECONDS_PER_YEAR);
    }

    function clamp(int256 x, uint256 cap) internal pure returns (int256) {
        int256 c = int256(cap);
        if (x > c) return c;
        if (x < -c) return -c;
        return x;
    }
}
