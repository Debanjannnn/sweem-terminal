// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {Test} from "forge-std/Test.sol";
import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";

import {SUSD} from "../src/tokens/SUSD.sol";
import {SweemVault} from "../src/SweemVault.sol";
import {IndexOracle} from "../src/IndexOracle.sol";
import {MarketRegistry} from "../src/MarketRegistry.sol";
import {IndexPerpEngine} from "../src/IndexPerpEngine.sol";

/// @dev Tests advance time with forge-std `skip()`, never `vm.warp` + a cached
///      `block.timestamp`: under via_ir the optimizer sinks `block.timestamp` reads
///      across the warp, so a value read before the warp is still the old one after.
contract BaseTest is Test {
    uint256 internal constant WAD = 1e18;
    uint256 internal constant YEAR = 365 days;

    bytes32 internal constant VOL = keccak256("VOL-ETH-USD");
    bytes32 internal constant POW2 = keccak256("POW2-ETH-USD");

    // The oracle service's canonical feed ids. feedId = keccak256(bytes(these)).
    string internal constant VOL_FEED = "sweem:vol:ETH/USD";
    string internal constant POW2_FEED = "sweem:pow:ETH/USD:k=2000000000000000000";

    // Cached: reading feedId() inside a vm.prank(...) argument list would consume the prank.
    bytes32 internal constant VOL_FEED_ID = keccak256(bytes(VOL_FEED));
    bytes32 internal constant POW2_FEED_ID = keccak256(bytes(POW2_FEED));

    address internal admin = address(0xA11CE);
    address internal alice = address(0xA1);
    address internal bob = address(0xB0B);
    address internal lp = address(0x11);
    address internal keeper = address(0xC0FFEE);

    SUSD internal susd;
    SweemVault internal vault;
    MarketRegistry internal registry;
    IndexPerpEngine internal engine;
    IndexOracle internal volOracle;
    IndexOracle internal pow2Oracle;

    function setUp() public virtual {
        susd = new SUSD();
        vm.startPrank(admin);
        vault = new SweemVault(
            IERC20(address(susd)), admin, 5_000_000e6, 0.8e18, uint256(0.02e18) / YEAR, uint256(0.2e18) / YEAR
        );
        registry = new MarketRegistry(admin);
        engine = new IndexPerpEngine(vault, registry, admin);
        vault.grantRole(vault.ENGINE_ROLE(), address(engine));

        volOracle = new IndexOracle(VOL_FEED, 1 hours, 3_000, admin);
        pow2Oracle = new IndexOracle(POW2_FEED, 1 hours, 2_000, admin);

        registry.addMarket(VOL, _params(volOracle, IndexOracle(address(0)), 0, 3e18, 800));
        registry.addMarket(POW2, _params(pow2Oracle, volOracle, 2e18, 4e18, 625));

        volOracle.pushIndex(VOL_FEED_ID, 0.8e18);
        pow2Oracle.pushIndex(POW2_FEED_ID, 1e18);
        vm.stopPrank();

        _fund(lp, 2_000_000e6);
        _fund(alice, 1_000_000e6);
        _fund(bob, 1_000_000e6);

        vm.prank(lp);
        vault.deposit(1_000_000e6, lp);
    }

    function _params(IndexOracle idx, IndexOracle vol, int256 k, uint256 maxLev, uint256 mmBps)
        internal
        pure
        returns (MarketRegistry.MarketParams memory)
    {
        return MarketRegistry.MarketParams({
            indexOracle: idx,
            volOracle: vol,
            k: k,
            maxOiPerSide: 500_000e18,
            maxLeverage: maxLev,
            minPositionSize: 10e18,
            maintenanceMarginBps: mmBps,
            openFeeBps: 10,
            closeFeeBps: 10,
            liquidationFeeBps: 500,
            fundingK: 1e18,
            fundingCapPerSec: uint256(1e18) / YEAR * 100,
            skewScale: 1_000_000e18,
            enabled: true
        });
    }

    function _fund(address who, uint256 amount) internal {
        susd.mint(who, amount);
        vm.prank(who);
        susd.approve(address(engine), type(uint256).max);
        vm.prank(who);
        susd.approve(address(vault), type(uint256).max);
    }

    /// @dev reset() bypasses the deviation breaker, so tests can move price freely.
    function _publish(IndexOracle o, uint256 v) internal {
        vm.prank(admin);
        o.reset(v);
    }

    /// @dev startPrank, because reading WRITER_ROLE() would otherwise consume the prank.
    function _grantWriter(IndexOracle o, address who) internal {
        vm.startPrank(admin);
        o.grantRole(o.WRITER_ROLE(), who);
        vm.stopPrank();
    }

    /// @dev The real hot-key write path.
    function _push(IndexOracle o, uint256 v) internal {
        bytes32 f = o.feedId();
        vm.prank(admin);
        o.pushIndex(f, v);
    }
}
