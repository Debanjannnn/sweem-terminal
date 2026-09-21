// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {SafeERC20} from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import {AccessControl} from "@openzeppelin/contracts/access/AccessControl.sol";
import {Pausable} from "@openzeppelin/contracts/utils/Pausable.sol";
import {ReentrancyGuard} from "@openzeppelin/contracts/utils/ReentrancyGuard.sol";

import {SweemVault} from "./SweemVault.sol";
import {MarketRegistry} from "./MarketRegistry.sol";
import {IndexOracle} from "./IndexOracle.sol";
import {Accrual} from "./libraries/Accrual.sol";

/// @notice Generic index perp engine. One contract, N markets. The vault is the
///         counterparty to every position — there is no order book, so the mark
///         price is derived from vault skew.
/// @dev Internal accounting is 1e18 ("wad") everywhere. USDC is 6 decimals and is
///      converted explicitly at the contract boundary, never implicitly.
///      Borrow and funding accrue through global cumulative indices — O(1), no loops.
contract IndexPerpEngine is AccessControl, Pausable, ReentrancyGuard {
    using SafeERC20 for IERC20;

    bytes32 public constant GOVERNOR_ROLE = keccak256("GOVERNOR_ROLE");
    bytes32 public constant GUARDIAN_ROLE = keccak256("GUARDIAN_ROLE");

    uint256 internal constant WAD = 1e18;
    uint256 internal constant BPS = 10_000;

    struct Position {
        int256 size; // signed, 1e18 index units
        uint256 collateral; // 1e18 USD
        uint256 entryIndex; // 1e18
        uint256 entryNotional; // 1e18 USD, accrual base
        uint256 entryBorrowCum; // 1e18
        int256 entryFundingCum; // 1e18
    }

    struct MarketState {
        uint256 borrowCum; // cumulative borrow per unit notional, 1e18
        int256 fundingCum; // cumulative funding per unit notional, 1e18 (positive: longs paid)
        uint256 longOI; // notional, 1e18
        uint256 shortOI; // notional, 1e18
        uint256 lastAccrual;
        bool halted; // market-level kill switch
    }

    SweemVault public immutable vault;
    IERC20 public immutable collateralToken;
    MarketRegistry public immutable registry;

    mapping(bytes32 => MarketState) public marketState;
    mapping(bytes32 => mapping(address => Position)) private _positions;

    /// @notice sum of longOI + shortOI across all markets, 1e18
    uint256 public totalOpenNotional;
    /// @notice sum of every position's collateral, 1e18
    uint256 public totalCollateral;

    event PositionOpened(
        bytes32 indexed marketId,
        address indexed trader,
        int256 size,
        uint256 collateral,
        uint256 entryIndex,
        uint256 notional,
        uint256 fee
    );
    event PositionModified(
        bytes32 indexed marketId,
        address indexed trader,
        int256 sizeDelta,
        int256 newSize,
        int256 collateralDelta,
        uint256 newCollateral,
        uint256 entryIndex,
        uint256 notional,
        uint256 fee
    );
    event PositionClosed(
        bytes32 indexed marketId,
        address indexed trader,
        int256 size,
        uint256 exitIndex,
        int256 realizedPnl,
        uint256 payout,
        uint256 fee
    );
    event PositionLiquidated(
        bytes32 indexed marketId,
        address indexed trader,
        address indexed liquidator,
        int256 size,
        uint256 exitIndex,
        uint256 penalty,
        uint256 liquidatorReward,
        uint256 traderRefund
    );
    event PositionSettled(
        bytes32 indexed marketId,
        address indexed trader,
        int256 pnl,
        uint256 borrowPaid,
        int256 fundingPaid,
        uint256 badDebt,
        uint256 newCollateral
    );
    event AccrualUpdated(
        bytes32 indexed marketId,
        uint256 dt,
        uint256 borrowRatePerSec,
        int256 fundingRatePerSec,
        int256 powerFundingPerSec,
        uint256 borrowCum,
        int256 fundingCum,
        uint256 markPrice,
        uint256 indexPrice
    );
    event OpenInterestChanged(bytes32 indexed marketId, uint256 longOI, uint256 shortOI, uint256 totalOpenNotional);
    event MarketHalted(bytes32 indexed marketId, address indexed guardian, bool halted);

    error MarketDisabled();
    error MarketIsHalted();
    error PositionExists();
    error NoPosition();
    error PositionTooSmall();
    error LeverageTooHigh();
    error OiCapExceeded();
    error StillHealthy();
    error InsufficientCollateral();
    error ZeroSize();

    constructor(SweemVault vault_, MarketRegistry registry_, address admin) {
        vault = vault_;
        registry = registry_;
        collateralToken = IERC20(vault_.asset());
        _grantRole(DEFAULT_ADMIN_ROLE, admin);
        _grantRole(GOVERNOR_ROLE, admin);
        _grantRole(GUARDIAN_ROLE, admin);
    }

    /// @notice Advance a market's borrow and funding cumulative indices. Permissionless.
    function accrue(bytes32 marketId) public {
        MarketRegistry.MarketParams memory p = registry.params(marketId);
        _accrue(marketId, p);
    }

    function _accrue(bytes32 marketId, MarketRegistry.MarketParams memory p) internal {
        MarketState storage m = marketState[marketId];
        if (m.lastAccrual == 0) {
            m.lastAccrual = block.timestamp;
            return;
        }
        uint256 dt = block.timestamp - m.lastAccrual;
        if (dt == 0) return;
        m.lastAccrual = block.timestamp;

        uint256 borrowRate = vault.borrowRatePerSecond();
        m.borrowCum += borrowRate * dt;

        int256 skewRate = Accrual.skewFundingRate(m.longOI, m.shortOI, p.skewScale, p.fundingK, p.fundingCapPerSec);
        int256 powerRate = _powerFundingRate(p);
        int256 fundingRate = Accrual.clamp(skewRate + powerRate, p.fundingCapPerSec);
        m.fundingCum += fundingRate * int256(dt);

        uint256 idx = _peekIndex(p);
        emit AccrualUpdated(
            marketId,
            dt,
            borrowRate,
            fundingRate,
            powerRate,
            m.borrowCum,
            m.fundingCum,
            Accrual.markPrice(idx, m.longOI, m.shortOI, p.skewScale),
            idx
        );
    }

    /// @notice The coupling: a power perp on P^k requires k(k-1)/2 * sigma^2 funding
    ///         per unit time, and sigma is exactly the volatility perp's index.
    /// @dev A stale/halted vol feed degrades to a zero power term rather than bricking
    ///      close and liquidate. Opening is gated on vol-feed health instead.
    function _powerFundingRate(MarketRegistry.MarketParams memory p) internal view returns (int256) {
        if (p.k == 0 || address(p.volOracle) == address(0)) return 0;
        try p.volOracle.read() returns (uint256 sigma, uint256) {
            return Accrual.powerFundingRate(p.k, sigma);
        } catch {
            return 0;
        }
    }

    function _peekIndex(MarketRegistry.MarketParams memory p) internal view returns (uint256) {
        try p.indexOracle.read() returns (uint256 v, uint256) {
            return v;
        } catch {
            return p.indexOracle.value();
        }
    }

    function openPosition(bytes32 marketId, int256 size, uint256 collateralAssets) external nonReentrant whenNotPaused {
        if (size == 0) revert ZeroSize();
        MarketRegistry.MarketParams memory p = _liveMarket(marketId);
        _requireVolFeedHealthy(p);
        Position storage pos = _positions[marketId][msg.sender];
        if (pos.size != 0) revert PositionExists();

        _accrue(marketId, p);
        (uint256 idx,) = p.indexOracle.read();

        _pullCollateral(collateralAssets);
        pos.collateral += vault.toWad(collateralAssets);
        totalCollateral += vault.toWad(collateralAssets);

        uint256 notional = _notional(size, idx);
        uint256 fee = (notional * p.openFeeBps) / BPS;
        _chargeFee(pos, fee);

        pos.size = size;
        _addOi(marketId, size, notional);
        _snapshot(marketId, pos, idx, notional);

        _validate(marketId, p, pos, idx);
        _rebalance(0);
        emit PositionOpened(marketId, msg.sender, size, pos.collateral, idx, notional, fee);
    }

    /// @param sizeDelta signed change in position size, 1e18
    /// @param collateralDeltaAssets signed change in collateral, 6-decimal asset units
    function modifyPosition(bytes32 marketId, int256 sizeDelta, int256 collateralDeltaAssets)
        external
        nonReentrant
        whenNotPaused
    {
        MarketRegistry.MarketParams memory p = _liveMarket(marketId);
        Position storage pos = _positions[marketId][msg.sender];
        if (pos.size == 0) revert NoPosition();
        if (sizeDelta > 0) _requireVolFeedHealthy(p);

        _accrue(marketId, p);
        (uint256 idx,) = p.indexOracle.read();
        _settle(marketId, msg.sender, pos, idx);

        _removeOi(marketId, pos.size, pos.entryNotional);

        if (collateralDeltaAssets > 0) {
            uint256 add = uint256(collateralDeltaAssets);
            _pullCollateral(add);
            pos.collateral += vault.toWad(add);
            totalCollateral += vault.toWad(add);
        }

        int256 newSize = pos.size + sizeDelta;
        if (newSize == 0) revert ZeroSize();

        uint256 fee = (_notional(sizeDelta, idx) * (sizeDelta > 0 ? p.openFeeBps : p.closeFeeBps)) / BPS;
        _chargeFee(pos, fee);

        uint256 withdrawAssets;
        if (collateralDeltaAssets < 0) {
            withdrawAssets = uint256(-collateralDeltaAssets);
            uint256 rm = vault.toWad(withdrawAssets);
            if (rm > pos.collateral) revert InsufficientCollateral();
            pos.collateral -= rm;
            totalCollateral -= rm;
        }

        pos.size = newSize;
        uint256 notional = _notional(newSize, idx);
        _addOi(marketId, newSize, notional);
        _snapshot(marketId, pos, idx, notional);

        _validate(marketId, p, pos, idx);
        _rebalance(withdrawAssets);
        if (withdrawAssets > 0) collateralToken.safeTransfer(msg.sender, withdrawAssets);
        emit PositionModified(
            marketId, msg.sender, sizeDelta, newSize, collateralDeltaAssets, pos.collateral, idx, notional, fee
        );
    }

    function closePosition(bytes32 marketId) external nonReentrant {
        MarketRegistry.MarketParams memory p = registry.params(marketId);
        Position storage pos = _positions[marketId][msg.sender];
        if (pos.size == 0) revert NoPosition();

        _accrue(marketId, p);
        (uint256 idx,) = p.indexOracle.read();
        int256 pnl = _settle(marketId, msg.sender, pos, idx);

        int256 size = pos.size;
        uint256 fee = (_notional(size, idx) * p.closeFeeBps) / BPS;
        _chargeFee(pos, fee);

        _removeOi(marketId, size, pos.entryNotional);
        uint256 payout = _clear(marketId, msg.sender, pos);
        _rebalance(payout);
        if (payout > 0) collateralToken.safeTransfer(msg.sender, payout);
        emit PositionClosed(marketId, msg.sender, size, idx, pnl, payout, fee);
    }

    /// @notice Liquidate an undercollateralized position. Permissionless.
    function liquidate(bytes32 marketId, address trader) external nonReentrant {
        MarketRegistry.MarketParams memory p = registry.params(marketId);
        Position storage pos = _positions[marketId][trader];
        if (pos.size == 0) revert NoPosition();

        _accrue(marketId, p);
        (uint256 idx,) = p.indexOracle.read();
        _settle(marketId, trader, pos, idx);

        uint256 notional = _notional(pos.size, idx);
        if (_marginRatio(pos.collateral, notional) >= p.maintenanceMarginBps * 1e14) revert StillHealthy();

        uint256 penalty = (notional * p.liquidationFeeBps) / BPS;
        if (penalty > pos.collateral) penalty = pos.collateral;
        pos.collateral -= penalty;
        totalCollateral -= penalty; // half to the liquidator, half retained by the vault

        uint256 reward = penalty / 2;
        int256 size = pos.size;
        _removeOi(marketId, size, pos.entryNotional);
        uint256 refund = _clear(marketId, trader, pos);
        uint256 rewardAssets = vault.toAsset(reward);
        _rebalance(refund + rewardAssets);
        if (refund > 0) collateralToken.safeTransfer(trader, refund);
        if (rewardAssets > 0) collateralToken.safeTransfer(msg.sender, rewardAssets);
        emit PositionLiquidated(marketId, trader, msg.sender, size, idx, penalty, reward, refund);
    }

    /// @dev Realize PnL, borrow and funding into collateral. Bad debt is absorbed by LPs.
    function _settle(bytes32 marketId, address trader, Position storage pos, uint256 idx)
        internal
        returns (int256 pnl)
    {
        MarketState storage m = marketState[marketId];

        pnl = (pos.size * (int256(idx) - int256(pos.entryIndex))) / int256(WAD);
        uint256 borrowOwed = Accrual.owed(pos.entryNotional, m.borrowCum, pos.entryBorrowCum);
        int256 fundingCumDelta = Accrual.owedSigned(pos.entryNotional, m.fundingCum, pos.entryFundingCum);
        int256 fundingOwed = pos.size > 0 ? fundingCumDelta : -fundingCumDelta;

        int256 net = pnl - int256(borrowOwed) - fundingOwed;
        uint256 badDebt;
        if (net >= 0) {
            pos.collateral += uint256(net);
            totalCollateral += uint256(net);
        } else {
            uint256 loss = uint256(-net);
            if (loss > pos.collateral) {
                badDebt = loss - pos.collateral;
                totalCollateral -= pos.collateral;
                pos.collateral = 0;
                vault.recordBadDebt(badDebt);
            } else {
                pos.collateral -= loss;
                totalCollateral -= loss;
            }
        }

        emit PositionSettled(marketId, trader, pnl, borrowOwed, fundingOwed, badDebt, pos.collateral);

        pos.entryIndex = idx;
        pos.entryBorrowCum = m.borrowCum;
        pos.entryFundingCum = m.fundingCum;
    }

    function _snapshot(bytes32 marketId, Position storage pos, uint256 idx, uint256 notional) internal {
        MarketState storage m = marketState[marketId];
        pos.entryIndex = idx;
        pos.entryNotional = notional;
        pos.entryBorrowCum = m.borrowCum;
        pos.entryFundingCum = m.fundingCum;
    }

    /// @dev Clears the position and returns what the trader is owed, in asset units.
    ///      The transfer itself is deferred until after _rebalance has topped the
    ///      engine up from the vault.
    function _clear(bytes32 marketId, address trader, Position storage pos) internal returns (uint256 payout) {
        payout = vault.toAsset(pos.collateral);
        totalCollateral -= pos.collateral;
        delete _positions[marketId][trader];
    }

    function _chargeFee(Position storage pos, uint256 fee) internal {
        if (fee == 0) return;
        if (fee > pos.collateral) fee = pos.collateral;
        pos.collateral -= fee;
        totalCollateral -= fee; // swept to the vault by _rebalance
    }

    function _pullCollateral(uint256 assets) internal {
        if (assets == 0) return;
        collateralToken.safeTransferFrom(msg.sender, address(this), assets);
    }

    /// @dev Keep the engine's USDC balance exactly equal to ceil(totalCollateral).
    ///      Any surplus (fees, trader losses, liquidation share) flows to the vault;
    ///      any shortfall (trader profit) is drawn from the vault. Exact, drift-free.
    function _rebalance(uint256 pendingOutAssets) internal {
        uint256 required = (totalCollateral + 1e12 - 1) / 1e12 + pendingOutAssets; // ceil to 6 decimals
        uint256 held = collateralToken.balanceOf(address(this));
        if (held < required) {
            vault.payOut(address(this), (required - held) * 1e12);
        } else if (held > required) {
            collateralToken.safeTransfer(address(vault), held - required);
        }
    }

    function _addOi(bytes32 marketId, int256 size, uint256 notional) internal {
        MarketState storage m = marketState[marketId];
        if (size > 0) m.longOI += notional;
        else m.shortOI += notional;
        totalOpenNotional += notional;
        vault.syncOpenNotional(totalOpenNotional);
        emit OpenInterestChanged(marketId, m.longOI, m.shortOI, totalOpenNotional);
    }

    function _removeOi(bytes32 marketId, int256 size, uint256 notional) internal {
        MarketState storage m = marketState[marketId];
        if (size > 0) m.longOI = m.longOI > notional ? m.longOI - notional : 0;
        else m.shortOI = m.shortOI > notional ? m.shortOI - notional : 0;
        totalOpenNotional = totalOpenNotional > notional ? totalOpenNotional - notional : 0;
        vault.syncOpenNotional(totalOpenNotional);
        emit OpenInterestChanged(marketId, m.longOI, m.shortOI, totalOpenNotional);
    }

    /// @dev Every risk limit lives here, in the engine — never in the UI.
    ///      (The vault utilization cap is enforced by SweemVault.syncOpenNotional.)
    function _validate(bytes32 marketId, MarketRegistry.MarketParams memory p, Position storage pos, uint256 idx)
        internal
        view
    {
        uint256 notional = _notional(pos.size, idx);
        if (notional < p.minPositionSize) revert PositionTooSmall();
        if (pos.collateral == 0 || notional * WAD > pos.collateral * p.maxLeverage) revert LeverageTooHigh();
        if (_marginRatio(pos.collateral, notional) < p.maintenanceMarginBps * 1e14) revert LeverageTooHigh();

        MarketState storage m = marketState[marketId];
        if (m.longOI > p.maxOiPerSide || m.shortOI > p.maxOiPerSide) revert OiCapExceeded();
    }

    function _requireVolFeedHealthy(MarketRegistry.MarketParams memory p) internal view {
        if (p.k != 0 && address(p.volOracle) != address(0)) p.volOracle.read();
    }

    function _liveMarket(bytes32 marketId) internal view returns (MarketRegistry.MarketParams memory p) {
        p = registry.params(marketId);
        if (!p.enabled) revert MarketDisabled();
        if (marketState[marketId].halted) revert MarketIsHalted();
    }

    function _notional(int256 size, uint256 idx) internal pure returns (uint256) {
        uint256 abs = size >= 0 ? uint256(size) : uint256(-size);
        return (abs * idx) / WAD;
    }

    function _marginRatio(uint256 collateral, uint256 notional) internal pure returns (uint256) {
        if (notional == 0) return type(uint256).max;
        return (collateral * WAD) / notional;
    }

    function getPosition(bytes32 marketId, address trader) external view returns (Position memory) {
        return _positions[marketId][trader];
    }

    function haltMarket(bytes32 marketId, bool halted) external onlyRole(GUARDIAN_ROLE) {
        marketState[marketId].halted = halted;
        emit MarketHalted(marketId, msg.sender, halted);
    }

    function pause() external onlyRole(GUARDIAN_ROLE) {
        _pause();
    }

    function unpause() external onlyRole(GOVERNOR_ROLE) {
        _unpause();
    }
}
