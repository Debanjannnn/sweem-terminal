import { indexer, type IndexPublication, type OracleBreakerEvent } from "envio";
import { eventId } from "../ids.js";
import { emptyFeed } from "../oracle.js";

// publishedAt is the oracle's own observation time; timestamp is the finalized
// block time. the gap is what the staleness guard measures, so both are kept.
//
// a tripped breaker REJECTS the value rather than writing it: latestValue
// deliberately does not move on BreakerTripped.

indexer.onEvent({ contract: "IndexOracle", event: "IndexPublished" }, async ({ event, context }) => {
  const feed = await context.IndexFeed.getOrCreate(emptyFeed(event.srcAddress));

  context.IndexFeed.set({
    ...feed,
    latestValue: event.params.value,
    latestRoundId: BigInt(event.params.roundId),
    latestTimestamp: event.params.timestamp,
    publicationCount: feed.publicationCount + 1,
  });

  const publication: IndexPublication = {
    id: eventId(event.block.number, event.logIndex),
    feed_id: event.srcAddress,
    roundId: BigInt(event.params.roundId),
    value: event.params.value,
    writer: event.params.writer,
    publishedAt: event.params.timestamp,
    timestamp: BigInt(event.block.timestamp),
    blockNumber: BigInt(event.block.number),
    txHash: event.transaction.hash,
  };
  context.IndexPublication.set(publication);
});

indexer.onEvent({ contract: "IndexOracle", event: "BreakerTripped" }, async ({ event, context }) => {
  const feed = await context.IndexFeed.getOrCreate(emptyFeed(event.srcAddress));
  context.IndexFeed.set({
    ...feed,
    halted: true,
    breakerTripCount: feed.breakerTripCount + 1,
  });

  const trip: OracleBreakerEvent = {
    id: eventId(event.block.number, event.logIndex),
    feed_id: event.srcAddress,
    action: "TRIPPED",
    roundId: BigInt(event.params.roundId),
    lastValue: event.params.lastValue,
    newValue: event.params.rejectedValue,
    deviationBps: event.params.deviationBps,
    timestamp: BigInt(event.block.timestamp),
    blockNumber: BigInt(event.block.number),
    txHash: event.transaction.hash,
  };
  context.OracleBreakerEvent.set(trip);
});

indexer.onEvent({ contract: "IndexOracle", event: "BreakerReset" }, async ({ event, context }) => {
  const feed = await context.IndexFeed.getOrCreate(emptyFeed(event.srcAddress));
  context.IndexFeed.set({
    ...feed,
    halted: false,
    latestValue: event.params.value,
    latestTimestamp: event.params.timestamp,
  });

  const reset: OracleBreakerEvent = {
    id: eventId(event.block.number, event.logIndex),
    feed_id: event.srcAddress,
    action: "RESET",
    roundId: feed.latestRoundId,
    lastValue: feed.latestValue,
    newValue: event.params.value,
    deviationBps: 0n,
    timestamp: BigInt(event.block.timestamp),
    blockNumber: BigInt(event.block.number),
    txHash: event.transaction.hash,
  };
  context.OracleBreakerEvent.set(reset);
});
