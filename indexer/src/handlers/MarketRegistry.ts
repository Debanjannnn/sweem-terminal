import { indexer, type Market } from "envio";
import { getProtocol } from "../vault.js";
import { emptyFeed } from "../oracle.js";

// addMarket emits MarketAdded then MarketParamsUpdated in the same tx, so the
// zero defaults below are always overwritten before the block ends.

const ZERO = "0x0000000000000000000000000000000000000000";

// IndexOracle is one contract per feed, so addresses are not knowable at config
// time. the registry is the authority on which feeds exist.
indexer.contractRegister(
  { contract: "MarketRegistry", event: "MarketAdded" },
  async ({ event, context }) => {
    context.chain.IndexOracle.add(event.params.indexOracle);
    if (event.params.volOracle !== ZERO) context.chain.IndexOracle.add(event.params.volOracle);
  },
);

indexer.onEvent({ contract: "MarketRegistry", event: "MarketAdded" }, async ({ event, context }) => {
  const ts = BigInt(event.block.timestamp);
  const hasVol = event.params.volOracle !== ZERO;

  await context.IndexFeed.getOrCreate(emptyFeed(event.params.indexOracle));
  if (hasVol) await context.IndexFeed.getOrCreate(emptyFeed(event.params.volOracle));

  const market: Market = {
    id: event.params.marketId,
    indexOracle_id: event.params.indexOracle,
    volOracle_id: hasVol ? event.params.volOracle : undefined,
    k: event.params.k,
    maxLeverage: 0n,
    maxOiPerSide: 0n,
    minPositionSize: 0n,
    maintenanceMarginBps: 0n,
    openFeeBps: 0n,
    closeFeeBps: 0n,
    liquidationFeeBps: 0n,
    skewScale: 0n,
    fundingK: 0n,
    fundingCapPerSec: 0n,
    enabled: false,
    halted: false,
    longOI: 0n,
    shortOI: 0n,
    skew: 0n,
    cumulativeFundingPerNotional: 0n,
    cumulativeBorrowPerNotional: 0n,
    lastFundingRate: 0n,
    lastBorrowRate: 0n,
    lastUtilization: 0n,
    lastIndexPrice: 0n,
    lastMarkPrice: 0n,
    cumVolume: 0n,
    cumFees: 0n,
    cumFundingToVault: 0n,
    cumBorrowToVault: 0n,
    tradeCount: 0,
    openPositionCount: 0,
    liquidationCount: 0,
    createdAtTimestamp: ts,
    createdAtBlock: BigInt(event.block.number),
    updatedAtTimestamp: ts,
  };
  context.Market.set(market);

  const protocol = await getProtocol(context);
  context.Protocol.set({
    ...protocol,
    marketCount: protocol.marketCount + 1,
    updatedAtTimestamp: ts,
    updatedAtBlock: BigInt(event.block.number),
  });
});

indexer.onEvent(
  { contract: "MarketRegistry", event: "MarketParamsUpdated" },
  async ({ event, context }) => {
    const market = await context.Market.getOrThrow(event.params.marketId);
    const p = event.params.params;
    context.Market.set({
      ...market,
      k: p.k,
      maxLeverage: p.maxLeverage,
      maxOiPerSide: p.maxOiPerSide,
      minPositionSize: p.minPositionSize,
      maintenanceMarginBps: p.maintenanceMarginBps,
      openFeeBps: p.openFeeBps,
      closeFeeBps: p.closeFeeBps,
      liquidationFeeBps: p.liquidationFeeBps,
      skewScale: p.skewScale,
      fundingK: p.fundingK,
      fundingCapPerSec: p.fundingCapPerSec,
      enabled: p.enabled,
      updatedAtTimestamp: BigInt(event.block.timestamp),
    });
  },
);

indexer.onEvent(
  { contract: "MarketRegistry", event: "MarketEnabled" },
  async ({ event, context }) => {
    const market = await context.Market.getOrThrow(event.params.marketId);
    context.Market.set({
      ...market,
      enabled: event.params.enabled,
      updatedAtTimestamp: BigInt(event.block.timestamp),
    });
  },
);
