// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {Math} from "@openzeppelin/contracts/utils/math/Math.sol";
import {IKuruOrderBook} from "../interfaces/IKuruOrderBook.sol";

library KuruMath {
    error NoPrice();

    uint256 internal constant USDC_UNIT = 1e6;

    /// Mid in raw price ticks. One-sided book falls back to the side present.
    function mid(IKuruOrderBook m) internal view returns (uint256) {
        (uint32 bid, uint32 ask) = m.bestBidAsk();
        bool hasBid = bid != 0 && bid != type(uint32).max; // absent bid sentinel = uint32.max
        bool hasAsk = ask != 0;
        if (hasBid && hasAsk) return (uint256(bid) + ask) / 2;
        if (hasBid) return bid;
        if (hasAsk) return ask;
        revert NoPrice();
    }

    /// Raw base atoms -> raw USDC atoms at `price` ticks.
    function toUsdc(IKuruOrderBook m, uint256 baseAtoms, uint256 price) internal view returns (uint256) {
        (uint32 pricePrecision, uint96 sizePrecision,,,,,) = m.getMarketParams();
        // 10^baseDecimals == sizePrecision * baseSizeMultiplier
        return Math.mulDiv(baseAtoms, price * USDC_UNIT, uint256(pricePrecision) * sizePrecision * m.baseSizeMultiplier());
    }

    function valueAtMid(IKuruOrderBook m, uint256 baseAtoms) internal view returns (uint256) {
        return baseAtoms == 0 ? 0 : toUsdc(m, baseAtoms, mid(m));
    }

    /// Round base atoms down to the market's size step.
    function floorToLot(IKuruOrderBook m, uint256 baseAtoms) internal view returns (uint256) {
        return baseAtoms - baseAtoms % m.baseSizeMultiplier();
    }
}
