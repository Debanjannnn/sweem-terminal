// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

interface IKuruOrderBook {
    struct SwapResult {
        uint128 amountInUsed;
        uint128 amountOut;
    }

    function swap(uint40 userId, bool isBuy, uint128 amountIn, uint128 minAmountOut, uint64 deadline)
        external
        returns (SwapResult memory);
    function bestBidAsk() external view returns (uint32 bid, uint32 ask);
    function getMarketParams()
        external
        view
        returns (
            uint32 pricePrecision,
            uint96 sizePrecision,
            uint32 tickSize,
            uint96 minQuoteNotional,
            uint96 maxQuoteNotional,
            uint256 takerFeePps,
            uint256 makerFeePps
        );
    function baseSizeMultiplier() external view returns (uint256);
    function baseToken() external view returns (address);
    function quoteToken() external view returns (address);
}
