// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {ERC4626, IERC20, ERC20} from "@openzeppelin/contracts/token/ERC20/extensions/ERC4626.sol";
import {IERC20Metadata} from "@openzeppelin/contracts/token/ERC20/extensions/IERC20Metadata.sol";
import {SafeERC20} from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import {AccessControl} from "@openzeppelin/contracts/access/AccessControl.sol";
import {Pausable} from "@openzeppelin/contracts/utils/Pausable.sol";
import {Math} from "@openzeppelin/contracts/utils/math/Math.sol";
import {Accrual} from "./libraries/Accrual.sol";

/// @notice Single-asset USDC ERC-4626 vault. The vault is the counterparty to every
///         native perp position; LPs earn borrow, funding and liquidation fees.
/// @dev Asset is 6-decimal USDC; shares are 18-decimal via a decimals offset.
///      All rate/utilization math is 1e18.
contract SweemVault is ERC4626, AccessControl, Pausable {
    using SafeERC20 for IERC20;

    bytes32 public constant ENGINE_ROLE = keccak256("ENGINE_ROLE");
    bytes32 public constant GOVERNOR_ROLE = keccak256("GOVERNOR_ROLE");

    uint256 public constant WAD = 1e18;

    /// @notice TVL cap, in asset (6-decimal) units. 0 == uncapped.
    uint256 public tvlCap;
    /// @notice max utilization the engine may drive the vault to, 1e18
    uint256 public maxUtilization;
    /// @notice borrow curve, per-second 1e18
    uint256 public baseRatePerSecond;
    uint256 public slopePerSecond;
    /// @notice notional currently rented by the engine, 1e18 USD. Synced by the engine.
    uint256 public openNotional;
    /// @notice cumulative bad debt absorbed by LPs, 1e18 USD
    uint256 public badDebt;

    // LP deposits/withdrawals are the standard ERC-4626 Deposit/Withdraw events.
    event EnginePaidOut(address indexed engine, address indexed to, uint256 assets, uint256 amountWad);
    event OpenNotionalSynced(uint256 openNotional, uint256 utilization);
    event BadDebtRecorded(address indexed engine, uint256 amountWad, uint256 totalBadDebt);
    event VaultParamsUpdated(uint256 tvlCap, uint256 maxUtilization, uint256 baseRatePerSecond, uint256 slopePerSecond);

    error UtilizationCapExceeded();
    error InsufficientLiquidity();

    constructor(
        IERC20 asset_,
        address admin,
        uint256 tvlCap_,
        uint256 maxUtilization_,
        uint256 baseRatePerSecond_,
        uint256 slopePerSecond_
    ) ERC20("sweem LP USDC", "sUSDC") ERC4626(asset_) {
        tvlCap = tvlCap_;
        maxUtilization = maxUtilization_;
        baseRatePerSecond = baseRatePerSecond_;
        slopePerSecond = slopePerSecond_;
        _grantRole(DEFAULT_ADMIN_ROLE, admin);
        _grantRole(GOVERNOR_ROLE, admin);
    }

    /// @dev shares are 18-decimal; also mitigates the inflation attack.
    function _decimalsOffset() internal pure override returns (uint8) {
        return 12;
    }

    function maxDeposit(address) public view override returns (uint256) {
        if (paused()) return 0;
        if (tvlCap == 0) return type(uint256).max;
        uint256 assets = totalAssets();
        return assets >= tvlCap ? 0 : tvlCap - assets;
    }

    function maxMint(address receiver) public view override returns (uint256) {
        uint256 assets = maxDeposit(receiver);
        return assets == type(uint256).max ? type(uint256).max : previewDeposit(assets);
    }

    /// @dev LPs cannot withdraw capital that is rented out to open positions.
    function maxWithdraw(address owner) public view override returns (uint256) {
        if (paused()) return 0;
        return Math.min(super.maxWithdraw(owner), _freeAssets());
    }

    function maxRedeem(address owner) public view override returns (uint256) {
        if (paused()) return 0;
        return Math.min(super.maxRedeem(owner), previewWithdraw(_freeAssets()));
    }

    function _withdraw(address caller, address receiver, address owner, uint256 assets, uint256 shares)
        internal
        override
    {
        super._withdraw(caller, receiver, owner, assets, shares);
        _checkUtilization();
    }

    /// @param amountWad amount in 1e18 USD; converted to 6-decimal at the boundary.
    function payOut(address to, uint256 amountWad) external onlyRole(ENGINE_ROLE) whenNotPaused {
        uint256 assets = toAsset(amountWad);
        if (assets > totalAssets()) revert InsufficientLiquidity();
        IERC20(asset()).safeTransfer(to, assets);
        emit EnginePaidOut(msg.sender, to, assets, amountWad);
    }

    function syncOpenNotional(uint256 openNotional_) external onlyRole(ENGINE_ROLE) {
        openNotional = openNotional_;
        emit OpenNotionalSynced(openNotional_, utilization());
        _checkUtilization();
    }

    function recordBadDebt(uint256 amountWad) external onlyRole(ENGINE_ROLE) {
        badDebt += amountWad;
        emit BadDebtRecorded(msg.sender, amountWad, badDebt);
    }

    /// @notice openNotional / TVL, 1e18. Returns 0 on an empty vault with no OI.
    function utilization() public view returns (uint256) {
        uint256 tvl = toWad(totalAssets());
        if (tvl == 0) return openNotional == 0 ? 0 : type(uint256).max;
        return (openNotional * WAD) / tvl;
    }

    function borrowRatePerSecond() public view returns (uint256) {
        uint256 u = utilization();
        if (u > WAD) u = WAD; // rate curve saturates at 100%
        return Accrual.borrowRatePerSecond(baseRatePerSecond, slopePerSecond, u);
    }

    /// @dev 6-decimal asset -> 18-decimal internal. Explicit, never implicit.
    function toWad(uint256 assets) public pure returns (uint256) {
        return assets * 1e12;
    }

    /// @dev 18-decimal internal -> 6-decimal asset. Rounds down, so sub-unit dust on a
    ///      payout stays with the vault rather than being minted for the trader.
    function toAsset(uint256 amountWad) public pure returns (uint256) {
        return amountWad / 1e12;
    }

    function setParams(uint256 tvlCap_, uint256 maxUtilization_, uint256 baseRate_, uint256 slope_)
        external
        onlyRole(GOVERNOR_ROLE)
    {
        tvlCap = tvlCap_;
        maxUtilization = maxUtilization_;
        baseRatePerSecond = baseRate_;
        slopePerSecond = slope_;
        emit VaultParamsUpdated(tvlCap_, maxUtilization_, baseRate_, slope_);
    }

    function pause() external onlyRole(GOVERNOR_ROLE) {
        _pause();
    }

    function unpause() external onlyRole(GOVERNOR_ROLE) {
        _unpause();
    }

    function _freeAssets() private view returns (uint256) {
        uint256 assets = totalAssets();
        uint256 reserved = toAsset((openNotional * WAD) / _maxUtilOrOne());
        return assets > reserved ? assets - reserved : 0;
    }

    function _maxUtilOrOne() private view returns (uint256) {
        return maxUtilization == 0 ? WAD : maxUtilization;
    }

    function _checkUtilization() private view {
        if (maxUtilization != 0 && utilization() > maxUtilization) revert UtilizationCapExceeded();
    }
}
