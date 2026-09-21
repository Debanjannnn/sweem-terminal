import type { LpAccount, Vault, Protocol } from "envio";
import { WAD, divWad, mulWad } from "./scale.js";
import { VAULT_ID, PROTOCOL_ID } from "./ids.js";

// HyperIndex derives the context type per (contract, event) inside
// indexer.onEvent and exports no standalone handler-context type to name.
// every entity built below is still explicitly typed.
// biome-ignore lint: see above
export type Ctx = any;

export type VaultEventKind =
  | "DEPOSIT"
  | "WITHDRAW"
  | "BORROW_ACCRUAL"
  | "FUNDING_ACCRUAL"
  | "LIQUIDATION_FEE"
  | "TRADING_FEE"
  | "TRADER_PNL"
  | "BAD_DEBT";

const EMPTY_VAULT = (): Vault => ({
  id: VAULT_ID,
  totalAssets: 0n,
  totalShares: 0n,
  sharePrice: WAD,
  cumBorrowIncome: 0n,
  cumFundingIncome: 0n,
  cumLiquidationFeeIncome: 0n,
  cumTradingFeeIncome: 0n,
  cumTraderPnlPaid: 0n,
  cumBadDebt: 0n,
  cumDeposited: 0n,
  cumWithdrawn: 0n,
  lpCount: 0,
  updatedAtTimestamp: 0n,
  updatedAtBlock: 0n,
});

const EMPTY_PROTOCOL = (): Protocol => ({
  id: PROTOCOL_ID,
  tvl: 0n,
  sharePrice: WAD,
  openInterestLong: 0n,
  openInterestShort: 0n,
  utilization: 0n,
  cumVolume: 0n,
  cumFees: 0n,
  cumLiquidationVolume: 0n,
  cumBadDebt: 0n,
  tradeCount: 0,
  liquidationCount: 0,
  lpCount: 0,
  traderCount: 0,
  marketCount: 0,
  updatedAtTimestamp: 0n,
  updatedAtBlock: 0n,
});

export const getVault = (context: Ctx): Promise<Vault> => context.Vault.getOrCreate(EMPTY_VAULT());
export const getProtocol = (context: Ctx): Promise<Protocol> =>
  context.Protocol.getOrCreate(EMPTY_PROTOCOL());

// genesis pinned at 1.0 rather than dividing by zero
export const sharePriceOf = (totalAssets: bigint, totalShares: bigint): bigint =>
  totalShares === 0n ? WAD : divWad(totalAssets, totalShares);

// the only implementation of LP mark-to-market: consumers read the field
export const unrealizedPnlOf = (
  lp: Pick<LpAccount, "shares" | "costBasis">,
  sharePrice: bigint,
): bigint => mulWad(lp.shares, sharePrice) - lp.costBasis;

export const utilizationOf = (
  protocol: Pick<Protocol, "openInterestLong" | "openInterestShort">,
  vault: Pick<Vault, "totalAssets">,
): bigint => divWad(protocol.openInterestLong + protocol.openInterestShort, vault.totalAssets);

interface Ev {
  block: { number: number; timestamp: number };
}

export interface VaultDelta {
  kind: VaultEventKind;
  // signed change in vault assets, WAD
  assetsDelta: bigint;
  // signed change in share supply, WAD. only deposits/withdrawals move it.
  sharesDelta?: bigint;
}

// THE reconstruction. IndexPerpEngine never events vault asset movements: it
// pins its USDC balance to totalCollateral and sweeps the difference. the
// vault's income legs are rebuilt from the economic legs of position events
// instead, which is both exact and exactly the decomposition LP APY needs:
//
//   PositionSettled        -> +borrowPaid, +fundingPaid, -pnl, -badDebt
//   Opened/Modified/Closed  -> +fee
//   PositionLiquidated      -> +(penalty - liquidatorReward)
//   Deposit/Withdraw        -> +/-assets
//
// single write path for vault state, so the decomposition can never drift from
// the share price it explains.
export async function applyVaultDelta(
  context: Ctx,
  event: Ev,
  { kind, assetsDelta, sharesDelta = 0n }: VaultDelta,
): Promise<Vault> {
  const prev = await getVault(context);

  const totalAssets = prev.totalAssets + assetsDelta;
  const totalShares = prev.totalShares + sharesDelta;
  const sharePrice = sharePriceOf(totalAssets, totalShares);

  const next: Vault = {
    ...prev,
    totalAssets,
    totalShares,
    sharePrice,
    cumBorrowIncome: prev.cumBorrowIncome + (kind === "BORROW_ACCRUAL" ? assetsDelta : 0n),
    cumFundingIncome: prev.cumFundingIncome + (kind === "FUNDING_ACCRUAL" ? assetsDelta : 0n),
    cumLiquidationFeeIncome:
      prev.cumLiquidationFeeIncome + (kind === "LIQUIDATION_FEE" ? assetsDelta : 0n),
    cumTradingFeeIncome: prev.cumTradingFeeIncome + (kind === "TRADING_FEE" ? assetsDelta : 0n),
    // assetsDelta is -realizedPnl for TRADER_PNL, so negate back to "paid out"
    cumTraderPnlPaid: prev.cumTraderPnlPaid + (kind === "TRADER_PNL" ? -assetsDelta : 0n),
    cumBadDebt: prev.cumBadDebt + (kind === "BAD_DEBT" ? -assetsDelta : 0n),
    cumDeposited: prev.cumDeposited + (kind === "DEPOSIT" ? assetsDelta : 0n),
    cumWithdrawn: prev.cumWithdrawn + (kind === "WITHDRAW" ? -assetsDelta : 0n),
    updatedAtTimestamp: BigInt(event.block.timestamp),
    updatedAtBlock: BigInt(event.block.number),
  };
  context.Vault.set(next);

  // an LP's unrealized PnL moves when the share price moves, not when that LP
  // acts, so the whole book is revalued here.
  // ponytail: O(lpCount) writes per share-price move, fine at demo scale. if
  // lpCount grows, make it a Hasura computed field over Vault.sharePrice.
  if (sharePrice !== prev.sharePrice) {
    const book: LpAccount[] = await context.LpAccount.getWhere({ vault_id: { _eq: VAULT_ID } });
    for (const lp of book) {
      context.LpAccount.set({ ...lp, unrealizedPnl: unrealizedPnlOf(lp, sharePrice) });
    }
  }

  const protocol = await getProtocol(context);
  context.Protocol.set({
    ...protocol,
    tvl: totalAssets,
    sharePrice,
    utilization: utilizationOf(protocol, next),
    lpCount: next.lpCount,
    updatedAtTimestamp: BigInt(event.block.timestamp),
    updatedAtBlock: BigInt(event.block.number),
  });

  return next;
}
