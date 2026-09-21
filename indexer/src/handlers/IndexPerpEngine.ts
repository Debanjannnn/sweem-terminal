import {
  indexer,
  type Market,
  type Position,
  type PositionEvent,
  type Trader,
  type Liquidation,
  type FundingRatePoint,
  type BorrowRatePoint,
} from "envio";
import { mulWad, divWad, usdcToWad, toApr, abs } from "../scale.js";
import { eventId, positionId } from "../ids.js";
import { applyVaultDelta, getProtocol, getVault, utilizationOf, type Ctx } from "../vault.js";

// every value here is WAD except PositionModified.collateralDelta,
// PositionClosed.payout and PositionLiquidated.traderRefund, which are 6dp
// asset units. those three are the only conversions in this file.
//
// PositionSettled is the single source for PnL/borrow/funding.
// PositionClosed.realizedPnl is the same number from the same settle and is
// lifecycle history only — never added to vault accounting a second time.

const EMPTY_TRADER = (id: string, ts: bigint): Trader => ({
  id,
  realizedPnl: 0n,
  cumVolume: 0n,
  cumFeesPaid: 0n,
  cumBorrowPaid: 0n,
  cumFundingPaid: 0n,
  tradeCount: 0,
  liquidationCount: 0,
  openPositionCount: 0,
  firstTradeTimestamp: ts,
  updatedAtTimestamp: ts,
});

async function loadTrader(context: Ctx, id: string, ts: bigint): Promise<Trader> {
  const existing: Trader | undefined = await context.Trader.get(id);
  if (existing) return existing;
  const protocol = await getProtocol(context);
  context.Protocol.set({ ...protocol, traderCount: protocol.traderCount + 1 });
  return EMPTY_TRADER(id, ts);
}

const EMPTY_POSITION = (
  id: string,
  marketId: string,
  trader: string,
  ts: bigint,
  block: bigint,
): Position => ({
  id,
  market_id: marketId,
  trader_id: trader,
  size: 0n,
  collateral: 0n,
  entryIndex: 0n,
  notional: 0n,
  isOpen: false,
  liquidated: false,
  realizedPnl: 0n,
  borrowPaid: 0n,
  fundingPaid: 0n,
  feesPaid: 0n,
  badDebt: 0n,
  openedAtTimestamp: ts,
  openedAtBlock: block,
  closedAtTimestamp: undefined,
  closedAtBlock: undefined,
});

// OI is not touched here; it comes from OpenInterestChanged
async function bumpProtocol(
  context: Ctx,
  event: { block: { number: number; timestamp: number } },
  patch: { volume?: bigint; fees?: bigint; trades?: number; liquidations?: number },
): Promise<void> {
  const protocol = await getProtocol(context);
  context.Protocol.set({
    ...protocol,
    cumVolume: protocol.cumVolume + (patch.volume ?? 0n),
    cumFees: protocol.cumFees + (patch.fees ?? 0n),
    cumLiquidationVolume:
      protocol.cumLiquidationVolume + (patch.liquidations ? (patch.volume ?? 0n) : 0n),
    tradeCount: protocol.tradeCount + (patch.trades ?? 0),
    liquidationCount: protocol.liquidationCount + (patch.liquidations ?? 0),
    updatedAtTimestamp: BigInt(event.block.timestamp),
    updatedAtBlock: BigInt(event.block.number),
  });
}

indexer.onEvent(
  { contract: "IndexPerpEngine", event: "PositionOpened" },
  async ({ event, context }) => {
    const ts = BigInt(event.block.timestamp);
    const block = BigInt(event.block.number);
    const { marketId, trader, size, collateral, entryIndex, notional, fee } = event.params;
    const pid = positionId(marketId, trader);

    const market: Market = await context.Market.getOrThrow(marketId);
    const traderEntity = await loadTrader(context, trader, ts);
    const prev = (await context.Position.get(pid)) ?? EMPTY_POSITION(pid, marketId, trader, ts, block);

    context.Position.set({
      ...prev,
      size,
      collateral,
      entryIndex,
      notional,
      isOpen: true,
      liquidated: false,
      feesPaid: prev.feesPaid + fee,
      openedAtTimestamp: ts,
      openedAtBlock: block,
      closedAtTimestamp: undefined,
      closedAtBlock: undefined,
    });

    context.Market.set({
      ...market,
      cumVolume: market.cumVolume + notional,
      cumFees: market.cumFees + fee,
      tradeCount: market.tradeCount + 1,
      openPositionCount: market.openPositionCount + 1,
      lastIndexPrice: entryIndex,
      updatedAtTimestamp: ts,
    });

    context.Trader.set({
      ...traderEntity,
      cumVolume: traderEntity.cumVolume + notional,
      cumFeesPaid: traderEntity.cumFeesPaid + fee,
      tradeCount: traderEntity.tradeCount + 1,
      openPositionCount: traderEntity.openPositionCount + 1,
      updatedAtTimestamp: ts,
    });

    const pe: PositionEvent = {
      id: eventId(event.block.number, event.logIndex),
      position_id: pid,
      market_id: marketId,
      trader_id: trader,
      action: "OPEN",
      sizeDelta: size,
      collateralDelta: collateral,
      sizeAfter: size,
      collateralAfter: collateral,
      indexPrice: entryIndex,
      notional,
      realizedPnl: 0n,
      borrowPaid: 0n,
      fundingPaid: 0n,
      fee,
      timestamp: ts,
      blockNumber: block,
      logIndex: event.logIndex,
      txHash: event.transaction.hash,
    };
    context.PositionEvent.set(pe);

    await bumpProtocol(context, event, { volume: notional, fees: fee, trades: 1 });
    if (fee > 0n) await applyVaultDelta(context, event, { kind: "TRADING_FEE", assetsDelta: fee });
  },
);

indexer.onEvent(
  { contract: "IndexPerpEngine", event: "PositionModified" },
  async ({ event, context }) => {
    const ts = BigInt(event.block.timestamp);
    const {
      marketId, trader, sizeDelta, newSize, collateralDelta,
      newCollateral, entryIndex, notional, fee,
    } = event.params;
    const pid = positionId(marketId, trader);

    const market: Market = await context.Market.getOrThrow(marketId);
    const position: Position = await context.Position.getOrThrow(pid);
    const traderEntity = await loadTrader(context, trader, ts);

    // the only signed 6dp conversion in the codebase, so it is explicit
    const collateralDeltaWad =
      collateralDelta < 0n ? -usdcToWad(-collateralDelta) : usdcToWad(collateralDelta);
    const tradedNotional = mulWad(abs(sizeDelta), entryIndex);

    context.Position.set({
      ...position,
      size: newSize,
      collateral: newCollateral,
      entryIndex,
      notional,
      isOpen: true,
      feesPaid: position.feesPaid + fee,
    });

    context.Market.set({
      ...market,
      cumVolume: market.cumVolume + tradedNotional,
      cumFees: market.cumFees + fee,
      tradeCount: market.tradeCount + 1,
      lastIndexPrice: entryIndex,
      updatedAtTimestamp: ts,
    });

    context.Trader.set({
      ...traderEntity,
      cumVolume: traderEntity.cumVolume + tradedNotional,
      cumFeesPaid: traderEntity.cumFeesPaid + fee,
      tradeCount: traderEntity.tradeCount + 1,
      updatedAtTimestamp: ts,
    });

    const pe: PositionEvent = {
      id: eventId(event.block.number, event.logIndex),
      position_id: pid,
      market_id: marketId,
      trader_id: trader,
      action: "MODIFY",
      sizeDelta,
      collateralDelta: collateralDeltaWad,
      sizeAfter: newSize,
      collateralAfter: newCollateral,
      indexPrice: entryIndex,
      notional: tradedNotional,
      realizedPnl: 0n,
      borrowPaid: 0n,
      fundingPaid: 0n,
      fee,
      timestamp: ts,
      blockNumber: BigInt(event.block.number),
      logIndex: event.logIndex,
      txHash: event.transaction.hash,
    };
    context.PositionEvent.set(pe);

    await bumpProtocol(context, event, { volume: tradedNotional, fees: fee, trades: 1 });
    if (fee > 0n) await applyVaultDelta(context, event, { kind: "TRADING_FEE", assetsDelta: fee });
  },
);

indexer.onEvent(
  { contract: "IndexPerpEngine", event: "PositionClosed" },
  async ({ event, context }) => {
    const ts = BigInt(event.block.timestamp);
    const { marketId, trader, size, exitIndex, realizedPnl, payout, fee } = event.params;
    const pid = positionId(marketId, trader);

    const market: Market = await context.Market.getOrThrow(marketId);
    const position: Position = await context.Position.getOrThrow(pid);
    const traderEntity = await loadTrader(context, trader, ts);
    const notional = mulWad(abs(size), exitIndex);

    context.Position.set({
      ...position,
      size: 0n,
      collateral: 0n,
      notional: 0n,
      isOpen: false,
      feesPaid: position.feesPaid + fee,
      closedAtTimestamp: ts,
      closedAtBlock: BigInt(event.block.number),
    });

    context.Market.set({
      ...market,
      cumVolume: market.cumVolume + notional,
      cumFees: market.cumFees + fee,
      tradeCount: market.tradeCount + 1,
      openPositionCount: market.openPositionCount - (position.isOpen ? 1 : 0),
      lastIndexPrice: exitIndex,
      updatedAtTimestamp: ts,
    });

    context.Trader.set({
      ...traderEntity,
      cumVolume: traderEntity.cumVolume + notional,
      cumFeesPaid: traderEntity.cumFeesPaid + fee,
      tradeCount: traderEntity.tradeCount + 1,
      openPositionCount: traderEntity.openPositionCount - (position.isOpen ? 1 : 0),
      updatedAtTimestamp: ts,
    });

    const pe: PositionEvent = {
      id: eventId(event.block.number, event.logIndex),
      position_id: pid,
      market_id: marketId,
      trader_id: trader,
      action: "CLOSE",
      sizeDelta: -size,
      collateralDelta: -usdcToWad(payout),
      sizeAfter: 0n,
      collateralAfter: 0n,
      indexPrice: exitIndex,
      notional,
      // lifecycle record only; PositionSettled owns vault-side accounting
      realizedPnl,
      borrowPaid: 0n,
      fundingPaid: 0n,
      fee,
      timestamp: ts,
      blockNumber: BigInt(event.block.number),
      logIndex: event.logIndex,
      txHash: event.transaction.hash,
    };
    context.PositionEvent.set(pe);

    await bumpProtocol(context, event, { volume: notional, fees: fee, trades: 1 });
    if (fee > 0n) await applyVaultDelta(context, event, { kind: "TRADING_FEE", assetsDelta: fee });
  },
);

indexer.onEvent(
  { contract: "IndexPerpEngine", event: "PositionLiquidated" },
  async ({ event, context }) => {
    const ts = BigInt(event.block.timestamp);
    const { marketId, trader, liquidator, size, exitIndex, penalty, liquidatorReward, traderRefund } =
      event.params;
    const pid = positionId(marketId, trader);

    const market: Market = await context.Market.getOrThrow(marketId);
    const position: Position = await context.Position.getOrThrow(pid);
    const traderEntity = await loadTrader(context, trader, ts);

    const notional = mulWad(abs(size), exitIndex);
    // the vault keeps whatever the liquidator was not paid, rather than
    // assuming the split is exactly 50/50
    const vaultFee = penalty - liquidatorReward;

    context.Position.set({
      ...position,
      size: 0n,
      collateral: 0n,
      notional: 0n,
      isOpen: false,
      liquidated: true,
      feesPaid: position.feesPaid + penalty,
      closedAtTimestamp: ts,
      closedAtBlock: BigInt(event.block.number),
    });

    context.Market.set({
      ...market,
      cumVolume: market.cumVolume + notional,
      cumFees: market.cumFees + penalty,
      tradeCount: market.tradeCount + 1,
      liquidationCount: market.liquidationCount + 1,
      openPositionCount: market.openPositionCount - (position.isOpen ? 1 : 0),
      lastIndexPrice: exitIndex,
      updatedAtTimestamp: ts,
    });

    context.Trader.set({
      ...traderEntity,
      cumFeesPaid: traderEntity.cumFeesPaid + penalty,
      cumVolume: traderEntity.cumVolume + notional,
      tradeCount: traderEntity.tradeCount + 1,
      liquidationCount: traderEntity.liquidationCount + 1,
      openPositionCount: traderEntity.openPositionCount - (position.isOpen ? 1 : 0),
      updatedAtTimestamp: ts,
    });

    const id = eventId(event.block.number, event.logIndex);
    const pe: PositionEvent = {
      id,
      position_id: pid,
      market_id: marketId,
      trader_id: trader,
      action: "LIQUIDATE",
      sizeDelta: -size,
      collateralDelta: -usdcToWad(traderRefund),
      sizeAfter: 0n,
      collateralAfter: 0n,
      indexPrice: exitIndex,
      notional,
      realizedPnl: 0n,
      borrowPaid: 0n,
      fundingPaid: 0n,
      fee: penalty,
      timestamp: ts,
      blockNumber: BigInt(event.block.number),
      logIndex: event.logIndex,
      txHash: event.transaction.hash,
    };
    context.PositionEvent.set(pe);

    const liq: Liquidation = {
      id,
      position_id: pid,
      market_id: marketId,
      trader_id: trader,
      liquidator,
      size,
      indexPrice: exitIndex,
      notional,
      penalty,
      liquidatorReward,
      vaultFee,
      traderRefund: usdcToWad(traderRefund),
      timestamp: ts,
      blockNumber: BigInt(event.block.number),
      txHash: event.transaction.hash,
    };
    context.Liquidation.set(liq);

    await bumpProtocol(context, event, {
      volume: notional,
      fees: penalty,
      trades: 1,
      liquidations: 1,
    });
    if (vaultFee > 0n)
      await applyVaultDelta(context, event, { kind: "LIQUIDATION_FEE", assetsDelta: vaultFee });
  },
);

indexer.onEvent(
  { contract: "IndexPerpEngine", event: "PositionSettled" },
  async ({ event, context }) => {
    const ts = BigInt(event.block.timestamp);
    const { marketId, trader, pnl, borrowPaid, fundingPaid, badDebt, newCollateral } = event.params;
    const pid = positionId(marketId, trader);

    const market: Market = await context.Market.getOrThrow(marketId);
    const position: Position = await context.Position.getOrThrow(pid);
    const traderEntity = await loadTrader(context, trader, ts);

    context.Position.set({
      ...position,
      collateral: newCollateral,
      realizedPnl: position.realizedPnl + pnl,
      borrowPaid: position.borrowPaid + borrowPaid,
      fundingPaid: position.fundingPaid + fundingPaid,
      badDebt: position.badDebt + badDebt,
    });

    context.Market.set({
      ...market,
      cumBorrowToVault: market.cumBorrowToVault + borrowPaid,
      cumFundingToVault: market.cumFundingToVault + fundingPaid,
      updatedAtTimestamp: ts,
    });

    context.Trader.set({
      ...traderEntity,
      realizedPnl: traderEntity.realizedPnl + pnl,
      cumBorrowPaid: traderEntity.cumBorrowPaid + borrowPaid,
      cumFundingPaid: traderEntity.cumFundingPaid + fundingPaid,
      updatedAtTimestamp: ts,
    });

    // four separate legs so the LP return can be decomposed
    if (borrowPaid !== 0n)
      await applyVaultDelta(context, event, { kind: "BORROW_ACCRUAL", assetsDelta: borrowPaid });
    if (fundingPaid !== 0n)
      await applyVaultDelta(context, event, { kind: "FUNDING_ACCRUAL", assetsDelta: fundingPaid });
    if (pnl !== 0n) await applyVaultDelta(context, event, { kind: "TRADER_PNL", assetsDelta: -pnl });
    if (badDebt !== 0n)
      await applyVaultDelta(context, event, { kind: "BAD_DEBT", assetsDelta: -badDebt });
  },
);

indexer.onEvent(
  { contract: "IndexPerpEngine", event: "AccrualUpdated" },
  async ({ event, context }) => {
    const ts = BigInt(event.block.timestamp);
    const {
      marketId, dt, borrowRatePerSec, fundingRatePerSec, powerFundingPerSec,
      borrowCum, fundingCum, markPrice, indexPrice,
    } = event.params;

    const market: Market = await context.Market.getOrThrow(marketId);
    const [protocol, vault] = await Promise.all([getProtocol(context), getVault(context)]);
    const utilization = utilizationOf(protocol, vault);

    context.Market.set({
      ...market,
      cumulativeBorrowPerNotional: borrowCum,
      cumulativeFundingPerNotional: fundingCum,
      lastBorrowRate: borrowRatePerSec,
      lastFundingRate: fundingRatePerSec,
      lastUtilization: utilization,
      lastMarkPrice: markPrice,
      lastIndexPrice: indexPrice,
      updatedAtTimestamp: ts,
    });

    const id = eventId(event.block.number, event.logIndex);
    const funding: FundingRatePoint = {
      id,
      market_id: marketId,
      timestamp: ts,
      blockNumber: BigInt(event.block.number),
      dt,
      fundingRate: fundingRatePerSec,
      powerFundingRate: powerFundingPerSec,
      fundingRateApr: toApr(fundingRatePerSec),
      cumulativeFundingPerNotional: fundingCum,
      longOI: market.longOI,
      shortOI: market.shortOI,
      skew: market.skew,
      markPrice,
      indexPrice,
    };
    context.FundingRatePoint.set(funding);

    const borrow: BorrowRatePoint = {
      id,
      market_id: marketId,
      timestamp: ts,
      blockNumber: BigInt(event.block.number),
      dt,
      borrowRate: borrowRatePerSec,
      borrowRateApr: toApr(borrowRatePerSec),
      cumulativeBorrowPerNotional: borrowCum,
      utilization,
    };
    context.BorrowRatePoint.set(borrow);
  },
);

indexer.onEvent(
  { contract: "IndexPerpEngine", event: "OpenInterestChanged" },
  async ({ event, context }) => {
    const { marketId, longOI, shortOI, totalOpenNotional } = event.params;
    const market: Market = await context.Market.getOrThrow(marketId);

    context.Market.set({
      ...market,
      longOI,
      shortOI,
      skew: longOI - shortOI,
      updatedAtTimestamp: BigInt(event.block.timestamp),
    });

    // the engine is authoritative on protocol-wide OI; the long/short split is
    // per-market, so the protocol split is maintained by delta
    const [protocol, vault] = await Promise.all([getProtocol(context), getVault(context)]);
    context.Protocol.set({
      ...protocol,
      openInterestLong: protocol.openInterestLong - market.longOI + longOI,
      openInterestShort: protocol.openInterestShort - market.shortOI + shortOI,
      utilization: divWad(totalOpenNotional, vault.totalAssets),
      updatedAtTimestamp: BigInt(event.block.timestamp),
      updatedAtBlock: BigInt(event.block.number),
    });
  },
);

indexer.onEvent(
  { contract: "IndexPerpEngine", event: "MarketHalted" },
  async ({ event, context }) => {
    const market: Market = await context.Market.getOrThrow(event.params.marketId);
    context.Market.set({
      ...market,
      halted: event.params.halted,
      updatedAtTimestamp: BigInt(event.block.timestamp),
    });
  },
);
