// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {BaseTest} from "./Base.t.sol";
import {IndexOracle} from "../src/IndexOracle.sol";
import {IndexPerpEngine} from "../src/IndexPerpEngine.sol";

contract IndexOracleTest is BaseTest {
    function test_publishAndRead() public {
        vm.prank(admin);
        volOracle.pushIndex(VOL_FEED_ID, 0.82e18);
        (uint256 v, uint256 t) = volOracle.read();
        assertEq(v, 0.82e18);
        assertEq(t, block.timestamp);
        assertTrue(volOracle.isHealthy());
    }

    function test_onlyWriterCanPublish() public {
        vm.expectRevert();
        vm.prank(alice);
        volOracle.pushIndex(VOL_FEED_ID, 0.9e18);
    }

    function test_stalenessHaltsTheMarket() public {
        skip(2 hours);
        assertFalse(volOracle.isHealthy());
        vm.expectRevert(abi.encodeWithSelector(IndexOracle.OracleStale.selector, volOracle.updatedAt(), 1 hours));
        volOracle.read();

        vm.prank(alice);
        vm.expectRevert();
        engine.openPosition(VOL, 100_000e18, 10_000e6);
    }

    function test_deviationBreakerTripsAndHaltsTheMarket() public {
        uint256 before = volOracle.value();
        vm.prank(admin);
        volOracle.pushIndex(VOL_FEED_ID, 2e18); // +150%, far past the 30% cap

        assertTrue(volOracle.halted());
        assertEq(volOracle.value(), before, "rejected value must not be written");
        vm.expectRevert(IndexOracle.OracleHalted.selector);
        volOracle.read();

        vm.prank(alice);
        vm.expectRevert();
        engine.openPosition(VOL, 100_000e18, 10_000e6);
    }

    function test_haltedOracleRejectsFurtherPublishes() public {
        vm.startPrank(admin);
        volOracle.pushIndex(VOL_FEED_ID, 2e18);
        vm.expectRevert(IndexOracle.OracleHalted.selector);
        volOracle.pushIndex(VOL_FEED_ID, 0.81e18);
        vm.stopPrank();
    }

    function test_adminResetClearsBreaker() public {
        vm.startPrank(admin);
        volOracle.pushIndex(VOL_FEED_ID, 2e18);
        assertTrue(volOracle.halted());
        volOracle.reset(0.85e18);
        vm.stopPrank();
        assertFalse(volOracle.halted());
        (uint256 v,) = volOracle.read();
        assertEq(v, 0.85e18);
    }

    function test_updateWithinDeviationSucceeds() public {
        vm.prank(admin);
        volOracle.pushIndex(VOL_FEED_ID, 0.9e18); // +12.5%, inside 30%
        assertFalse(volOracle.halted());
        assertEq(volOracle.value(), 0.9e18);
    }

    function test_roundIdIncrements() public {
        uint80 r0 = volOracle.roundId();
        vm.prank(admin);
        volOracle.pushIndex(VOL_FEED_ID, 0.81e18);
        assertEq(volOracle.roundId(), r0 + 1);
    }

    function test_zeroValueRejected() public {
        vm.prank(admin);
        vm.expectRevert(IndexOracle.ZeroValue.selector);
        volOracle.pushIndex(VOL_FEED_ID, 0);
    }

    function testFuzz_breakerBoundary(uint256 newValue) public {
        uint256 last = volOracle.value();
        newValue = bound(newValue, 1, 10e18);
        uint256 diff = newValue > last ? newValue - last : last - newValue;
        bool shouldTrip = (diff * 10_000) / last > volOracle.maxDeviationBps();
        vm.prank(admin);
        volOracle.pushIndex(VOL_FEED_ID, newValue);
        assertEq(volOracle.halted(), shouldTrip);
    }

    function _report(bytes32 feed, uint256 v) internal pure returns (bytes memory) {
        return abi.encode(feed, v);
    }

    function test_onReportIsIndistinguishableFromDirectPush() public {
        // The forwarder is granted the same WRITER_ROLE as the hot key — no special case.
        address forwarder = address(0xF0B);
        _grantWriter(volOracle, forwarder);

        vm.prank(forwarder);
        volOracle.onReport("", _report(VOL_FEED_ID, 0.85e18));

        (uint256 v, uint64 upd, uint64 rid) = volOracle.latestRound(VOL_FEED_ID);
        assertEq(v, 0.85e18);
        assertEq(upd, uint64(block.timestamp));
        assertEq(rid, 2, "onReport must increment the round like any other write");
        assertEq(volOracle.value(), 0.85e18);
        assertTrue(volOracle.isHealthy());
    }

    function test_onReportTripsTheBreakerIdenticallyToDirectPush() public {
        address forwarder = address(0xF0B);
        _grantWriter(volOracle, forwarder);
        _grantWriter(pow2Oracle, forwarder);

        // same +150% move, one through each path, on two identically-configured feeds
        vm.prank(admin);
        pow2Oracle.setParams(1 hours, 3_000);

        uint64 volRoundBefore = volOracle.roundId();
        uint64 powRoundBefore = pow2Oracle.roundId();

        vm.prank(admin);
        volOracle.pushIndex(VOL_FEED_ID, 0.8e18 * 250 / 100);
        vm.prank(forwarder);
        pow2Oracle.onReport("", _report(POW2_FEED_ID, 1e18 * 250 / 100));

        assertTrue(volOracle.halted(), "direct push must trip");
        assertTrue(pow2Oracle.halted(), "onReport must trip identically");
        assertEq(volOracle.roundId(), volRoundBefore, "rejected value must not bump the round");
        assertEq(pow2Oracle.roundId(), powRoundBefore);
        assertEq(volOracle.value(), 0.8e18, "rejected value must not be written");
        assertEq(pow2Oracle.value(), 1e18);

        vm.expectRevert(IndexOracle.OracleHalted.selector);
        volOracle.read();
        vm.expectRevert(IndexOracle.OracleHalted.selector);
        pow2Oracle.read();
    }

    function test_onReportRejectsAMisroutedFeed() public {
        bytes32 wrong = POW2_FEED_ID;
        vm.expectRevert(abi.encodeWithSelector(IndexOracle.WrongFeed.selector, VOL_FEED_ID, wrong));
        vm.prank(admin);
        volOracle.onReport("", _report(wrong, 0.85e18));
        assertEq(volOracle.value(), 0.8e18, "misrouted report must not write");
    }

    function test_pushIndexRejectsAMisroutedFeed() public {
        bytes32 wrong = POW2_FEED_ID;
        vm.expectRevert(abi.encodeWithSelector(IndexOracle.WrongFeed.selector, VOL_FEED_ID, wrong));
        vm.prank(admin);
        volOracle.pushIndex(wrong, 0.85e18);
    }

    function test_latestRoundRejectsAMisroutedFeed() public {
        vm.expectRevert(abi.encodeWithSelector(IndexOracle.WrongFeed.selector, VOL_FEED_ID, POW2_FEED_ID));
        volOracle.latestRound(POW2_FEED_ID);
    }

    function test_onReportRequiresTheWriterRole() public {
        vm.expectRevert();
        vm.prank(alice);
        volOracle.onReport("", _report(VOL_FEED_ID, 0.85e18));
    }

    function test_feedIdIsKeccakOfTheCanonicalMarketId() public view {
        assertEq(volOracle.feedId(), keccak256(bytes(VOL_FEED)));
        assertEq(volOracle.feedId(), keccak256(bytes(volOracle.marketId())));
        assertEq(pow2Oracle.feedId(), keccak256(bytes(POW2_FEED)));
    }

    function test_pushIndexReturnsTheNewRoundId() public {
        vm.prank(admin);
        uint64 rid = volOracle.pushIndex(VOL_FEED_ID, 0.81e18);
        assertEq(rid, volOracle.roundId());
        assertEq(rid, 2);
    }

    /// @dev A power market whose vol feed is stale must refuse new risk, but must
    ///      still allow closing (the power funding term degrades to zero).
    function test_staleVolFeedBlocksOpenButNotClose() public {
        vm.prank(alice);
        engine.openPosition(POW2, 100_000e18, 50_000e6);

        skip(2 hours);
        _publish(pow2Oracle, 1e18); // index fresh, vol feed stale

        vm.prank(bob);
        vm.expectRevert();
        engine.openPosition(POW2, 100_000e18, 50_000e6);

        vm.prank(alice);
        engine.closePosition(POW2);
        assertEq(engine.getPosition(POW2, alice).size, 0);
    }
}
