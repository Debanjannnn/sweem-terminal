// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {AccessControl} from "@openzeppelin/contracts/access/AccessControl.sol";
import {IndexOracle} from "./IndexOracle.sol";

/// @notice Market parameter store. A market is (indexOracle, k, riskParams).
/// @dev Vol perps and power perps are the same market type; they differ only in
///      which oracle they point at and whether `volOracle`/`k` are set.
contract MarketRegistry is AccessControl {
    bytes32 public constant GOVERNOR_ROLE = keccak256("GOVERNOR_ROLE");

    struct MarketParams {
        IndexOracle indexOracle; // index the perp settles against
        IndexOracle volOracle; // annualized sigma feed; address(0) disables power funding
        int256 k; // power exponent, signed 1e18. 0 for a plain index perp
        uint256 maxOiPerSide; // notional cap per side, 1e18
        uint256 maxLeverage; // 1e18 (e.g. 5e18 == 5x)
        uint256 minPositionSize; // minimum notional, 1e18
        uint256 maintenanceMarginBps;
        uint256 openFeeBps;
        uint256 closeFeeBps;
        uint256 liquidationFeeBps; // penalty on notional at liquidation
        uint256 fundingK; // k_f, 1e18
        uint256 fundingCapPerSec; // absolute funding cap, 1e18 per second
        uint256 skewScale; // notional at which mark premium is 100%, 1e18
        bool enabled;
    }

    mapping(bytes32 => MarketParams) private _markets;

    event MarketAdded(bytes32 indexed marketId, address indexed indexOracle, address indexed volOracle, int256 k);
    event MarketParamsUpdated(bytes32 indexed marketId, MarketParams params);
    event MarketEnabled(bytes32 indexed marketId, bool enabled);

    error MarketExists();
    error UnknownMarket();
    error BadParams();

    constructor(address admin) {
        _grantRole(DEFAULT_ADMIN_ROLE, admin);
        _grantRole(GOVERNOR_ROLE, admin);
    }

    function addMarket(bytes32 marketId, MarketParams calldata p) external onlyRole(GOVERNOR_ROLE) {
        if (address(_markets[marketId].indexOracle) != address(0)) revert MarketExists();
        _validate(p);
        _markets[marketId] = p;
        emit MarketAdded(marketId, address(p.indexOracle), address(p.volOracle), p.k);
        emit MarketParamsUpdated(marketId, p);
    }

    function setParams(bytes32 marketId, MarketParams calldata p) external onlyRole(GOVERNOR_ROLE) {
        if (address(_markets[marketId].indexOracle) == address(0)) revert UnknownMarket();
        _validate(p);
        _markets[marketId] = p;
        emit MarketParamsUpdated(marketId, p);
    }

    function setEnabled(bytes32 marketId, bool enabled) external onlyRole(GOVERNOR_ROLE) {
        if (address(_markets[marketId].indexOracle) == address(0)) revert UnknownMarket();
        _markets[marketId].enabled = enabled;
        emit MarketEnabled(marketId, enabled);
    }

    function params(bytes32 marketId) external view returns (MarketParams memory p) {
        p = _markets[marketId];
        if (address(p.indexOracle) == address(0)) revert UnknownMarket();
    }

    function _validate(MarketParams calldata p) private pure {
        if (address(p.indexOracle) == address(0)) revert BadParams();
        if (p.maxLeverage == 0 || p.maxOiPerSide == 0 || p.skewScale == 0) revert BadParams();
        if (p.maintenanceMarginBps == 0 || p.maintenanceMarginBps >= 10_000) revert BadParams();
        if (p.openFeeBps > 1_000 || p.closeFeeBps > 1_000 || p.liquidationFeeBps > 2_000) revert BadParams();
        // power markets must reference a vol oracle for the k(k-1)/2 * sigma^2 funding term
        if (p.k != 0 && address(p.volOracle) == address(0)) revert BadParams();
    }
}
