import { indexer, type KuruSlot } from "envio";
import { kuruFillId, kuruSlotId } from "../ids.js";
import { decodeBookUpdates, decodeTrades } from "../kuru-packed.js";

// one OrderBook proxy per market, so srcAddress IS the market.
// ponytail: stored book only — no executable-depth model and no L2 level
// aggregation. add the depth model when something needs stale-RAB-adjusted
// depth, and it will need a per-order derived transition at minSizeAfterBlock+1
// that can be unscheduled.
// records are applied in byte order within a log, which is the tail of the
// canonical order (blockNumber, txIndex, logIndex, byteOffset). logIndex is
// block-scoped, so (blockNumber, logIndex, recordIdx) is the same total order
// with one fewer column to store.

indexer.onEvent(
  { contract: "KuruOrderBook", event: "BookUpdatesPacked" },
  async ({ event, context }) => {
    for (const r of decodeBookUpdates(event.params.packedUpdates)) {
      const id = kuruSlotId(event.srcAddress, r.userId, r.slotIdx);
      if (!r.live) {
        // cancel or replacement removal. it must match what we think is resting;
        // absent is fine (the slot predates start_block), contradicting is not.
        const slot = await context.KuruSlot.get(id);
        if (slot && (slot.orderId !== r.orderId || slot.price !== r.price || slot.isBuy !== r.isBuy))
          throw new Error(`kuru: delete of ${id} does not match the resting order ${slot.orderId}`);
        context.KuruSlot.deleteUnsafe(id);
        continue;
      }
      // a live record is a complete slot snapshot, not a delta
      const slot: KuruSlot = {
        id,
        market: event.srcAddress,
        userId: r.userId,
        slotIdx: r.slotIdx,
        orderId: r.orderId,
        price: r.price,
        size: r.size,
        isBuy: r.isBuy,
        minSizeAfterBlock: r.minSizeAfterBlock,
        makerFeePps: r.makerFeePps,
        updatedAtBlock: BigInt(event.block.number),
        updatedAtTimestamp: BigInt(event.block.timestamp),
      };
      context.KuruSlot.set(slot);
    }
  },
);

indexer.onEvent({ contract: "KuruOrderBook", event: "TradesPacked" }, async ({ event, context }) => {
  const records = decodeTrades(event.params.packedTrades);
  for (let i = 0; i < records.length; i++) {
    const r = records[i]!;
    context.KuruFill.set({
      id: kuruFillId(event.srcAddress, r.tradeId, i),
      market: event.srcAddress,
      tradeId: r.tradeId,
      recordIdx: i,
      takerId: event.params.accountId,
      executor: event.params.executor,
      makerId: r.makerId,
      slotIdx: r.slotIdx,
      orderId: r.orderId,
      makerIsBuy: r.makerIsBuy,
      passive: r.passive,
      matchEnd: r.matchEnd,
      price: r.price,
      filledSize: r.filledSize,
      updatedSize: r.updatedSize,
      makerFeePps: r.makerFeePps,
      takerFeePps: Number(event.params.effectiveTakerFeePps),
      clientOrderId: event.params.clientOrderId,
      timestamp: BigInt(event.block.timestamp),
      blockNumber: BigInt(event.block.number),
      logIndex: event.logIndex,
      txHash: event.transaction.hash,
    });

    // a passive fill has no slot: orderId carries the band's lowPrice
    if (r.passive) continue;

    // a pure fill emits no BookUpdatesPacked deletion, so the slot is settled
    // here. updatedSize is the post-state, not a delta: a stale-RAB touch or
    // dust cleanup releases unfilled size in the same match.
    const id = kuruSlotId(event.srcAddress, r.makerId, r.slotIdx);
    const slot = await context.KuruSlot.get(id);
    if (!slot) continue;
    if (slot.orderId !== r.orderId)
      throw new Error(`kuru: fill on ${id} names order ${r.orderId}, slot holds ${slot.orderId}`);
    if (r.updatedSize === 0n) context.KuruSlot.deleteUnsafe(id);
    else
      context.KuruSlot.set({
        ...slot,
        size: r.updatedSize,
        updatedAtBlock: BigInt(event.block.number),
        updatedAtTimestamp: BigInt(event.block.timestamp),
      });
  }
});
