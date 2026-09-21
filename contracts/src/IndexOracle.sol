// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {AccessControl} from "@openzeppelin/contracts/access/AccessControl.sol";

/// @notice Push oracle for a single index feed. Values are 1e18 fixed point.
/// @dev One instance per feed. `feedId` is `keccak256(bytes(marketId))`, where
///      `marketId` is the oracle service's canonical string (e.g. "sweem:vol:ETH/USD"),
///      and every write path carries it so a misrouted report cannot land here.
///
///      Two writers exist and they are deliberately indistinguishable: the Chainlink
///      keystone forwarder (`onReport`) and the fallback hot key (`pushIndex`) hold the
///      same WRITER_ROLE and run the same validation. That is what makes CRE optional
///      rather than load-bearing — losing CRE loses a bounty, not the product.
contract IndexOracle is AccessControl {
    bytes32 public constant WRITER_ROLE = keccak256("WRITER_ROLE");

    string public marketId;
    bytes32 public immutable feedId;

    uint256 public value;
    uint64 public updatedAt;
    uint64 public roundId;

    uint256 public maxStaleness; // seconds
    uint256 public maxDeviationBps; // per-update, vs previous value
    bool public halted;

    event IndexPublished(uint64 indexed roundId, uint256 value, uint256 timestamp, address indexed writer);
    event BreakerTripped(uint64 indexed roundId, uint256 lastValue, uint256 rejectedValue, uint256 deviationBps);
    event BreakerReset(address indexed admin, uint256 value, uint256 timestamp);
    event OracleParamsUpdated(uint256 maxStaleness, uint256 maxDeviationBps);

    error ZeroValue();
    error OracleHalted();
    error OracleStale(uint256 updatedAt, uint256 maxStaleness);
    error WrongFeed(bytes32 expected, bytes32 got);

    constructor(string memory marketId_, uint256 maxStaleness_, uint256 maxDeviationBps_, address admin) {
        marketId = marketId_;
        feedId = keccak256(bytes(marketId_));
        maxStaleness = maxStaleness_;
        maxDeviationBps = maxDeviationBps_;
        _grantRole(DEFAULT_ADMIN_ROLE, admin);
        _grantRole(WRITER_ROLE, admin);
    }

    /// @notice Direct-writer path, used by the oracle service's hot-key fallback.
    /// @return the round id after the write (unchanged if the breaker tripped)
    function pushIndex(bytes32 feedId_, uint256 newValue) external onlyRole(WRITER_ROLE) returns (uint64) {
        if (feedId_ != feedId) revert WrongFeed(feedId, feedId_);
        return _push(newValue);
    }

    /// @param report abi.encode(bytes32 feedId, uint256 value)
    function onReport(
        bytes calldata,
        /* metadata */
        bytes calldata report
    )
        external
        onlyRole(WRITER_ROLE)
    {
        (bytes32 feedId_, uint256 newValue) = abi.decode(report, (bytes32, uint256));
        if (feedId_ != feedId) revert WrongFeed(feedId, feedId_);
        _push(newValue);
    }

    function _push(uint256 newValue) internal returns (uint64) {
        if (newValue == 0) revert ZeroValue();
        if (halted) revert OracleHalted();

        uint256 last = value;
        if (last != 0) {
            uint256 diff = newValue > last ? newValue - last : last - newValue;
            uint256 devBps = (diff * 10_000) / last;
            if (devBps > maxDeviationBps) {
                halted = true;
                emit BreakerTripped(roundId, last, newValue, devBps);
                return roundId;
            }
        }

        value = newValue;
        updatedAt = uint64(block.timestamp);
        unchecked {
            roundId += 1;
        }
        emit IndexPublished(roundId, newValue, block.timestamp, msg.sender);
        return roundId;
    }

    /// @notice Raw latest round. Never reverts on staleness — the caller decides.
    /// @dev Output order matches the oracle service's shipped ABI: (value, updatedAt, roundId).
    function latestRound(bytes32 feedId_) external view returns (uint256 value_, uint64 updatedAt_, uint64 roundId_) {
        if (feedId_ != feedId) revert WrongFeed(feedId, feedId_);
        return (value, updatedAt, roundId);
    }

    /// @notice Fresh, un-halted index value. Reverts otherwise — the engine must not
    ///         settle against stale data.
    function read() external view returns (uint256 value_, uint256 updatedAt_) {
        if (halted) revert OracleHalted();
        if (updatedAt == 0 || block.timestamp - updatedAt > maxStaleness) {
            revert OracleStale(updatedAt, maxStaleness);
        }
        return (value, updatedAt);
    }

    function isHealthy() external view returns (bool) {
        return !halted && updatedAt != 0 && block.timestamp - updatedAt <= maxStaleness;
    }

    /// @notice Clear the breaker and seed a fresh value, bypassing the deviation check.
    function reset(uint256 newValue) external onlyRole(DEFAULT_ADMIN_ROLE) {
        if (newValue == 0) revert ZeroValue();
        halted = false;
        value = newValue;
        updatedAt = uint64(block.timestamp);
        unchecked {
            roundId += 1;
        }
        emit BreakerReset(msg.sender, newValue, block.timestamp);
    }

    function setParams(uint256 maxStaleness_, uint256 maxDeviationBps_) external onlyRole(DEFAULT_ADMIN_ROLE) {
        maxStaleness = maxStaleness_;
        maxDeviationBps = maxDeviationBps_;
        emit OracleParamsUpdated(maxStaleness_, maxDeviationBps_);
    }
}
