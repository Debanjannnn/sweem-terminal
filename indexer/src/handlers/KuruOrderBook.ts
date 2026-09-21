import { indexer, type KuruOrder, type KuruTrade } from "envio";
import { eventId, kuruOrderId } from "../ids.js";

// kuru has no fills API — these logs are the only spot trade history there is.
// one OrderBook proxy per market, so srcAddress IS the market.
// price/size stay in raw venue ticks: the per-market precision is not in any
// event, so scaling here would be a guess.

indexer.onEvent({ contract: "KuruOrderBook", event: "OrderCreated" }, async ({ event, context }) => {
  const ts = BigInt(event.block.timestamp);
  const order: KuruOrder = {
    id: kuruOrderId(event.srcAddress, event.params.orderId),
    market: event.srcAddress,
    orderId: event.params.orderId,
    owner: event.params.owner,
    price: event.params.price,
    size: event.params.size,
    remainingSize: event.params.size,
    isBuy: event.params.isBuy,
    status: "OPEN",
    createdAtTimestamp: ts,
    createdAtBlock: BigInt(event.block.number),
    updatedAtTimestamp: ts,
    txHash: event.transaction.hash,
  };
  context.KuruOrder.set(order);
});

indexer.onEvent({ contract: "KuruOrderBook", event: "Trade" }, async ({ event, context }) => {
  const ts = BigInt(event.block.timestamp);
  const oid = kuruOrderId(event.srcAddress, event.params.orderId);
  // undefined when the resting order was created before start_block
  const order: KuruOrder | undefined = await context.KuruOrder.get(oid);

  if (order) {
    context.KuruOrder.set({
      ...order,
      remainingSize: event.params.updatedSize,
      status: event.params.updatedSize === 0n ? "FILLED" : "OPEN",
      updatedAtTimestamp: ts,
    });
  }

  const trade: KuruTrade = {
    id: eventId(event.block.number, event.logIndex),
    market: event.srcAddress,
    order_id: order ? oid : undefined,
    orderId: event.params.orderId,
    maker: event.params.makerAddress,
    taker: event.params.takerAddress,
    txOrigin: event.params.txOrigin,
    isBuy: event.params.isBuy,
    price: event.params.price,
    filledSize: event.params.filledSize,
    updatedSize: event.params.updatedSize,
    timestamp: ts,
    blockNumber: BigInt(event.block.number),
    logIndex: event.logIndex,
    txHash: event.transaction.hash,
  };
  context.KuruTrade.set(trade);
});

indexer.onEvent(
  { contract: "KuruOrderBook", event: "OrdersCanceled" },
  async ({ event, context }) => {
    const ts = BigInt(event.block.timestamp);
    for (const id of event.params.orderId) {
      const order: KuruOrder | undefined = await context.KuruOrder.get(
        kuruOrderId(event.srcAddress, id),
      );
      if (!order) continue;
      context.KuruOrder.set({
        ...order,
        status: "CANCELED",
        remainingSize: 0n,
        updatedAtTimestamp: ts,
      });
    }
  },
);
