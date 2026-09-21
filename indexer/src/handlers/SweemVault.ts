import { indexer, type LpAccount, type VaultFlow } from "envio";
import { usdcToWad, sharesToWad, mulWad, divWad } from "../scale.js";
import { eventId, VAULT_ID } from "../ids.js";
import { applyVaultDelta, getVault, sharePriceOf, unrealizedPnlOf } from "../vault.js";

// LP realized PnL is average cost basis: a withdrawal realizes
// assetsOut - costBasis * (sharesOut / sharesHeld).

const EMPTY_LP = (id: string, ts: bigint): LpAccount => ({
  id,
  vault_id: VAULT_ID,
  shares: 0n,
  costBasis: 0n,
  cumDeposited: 0n,
  cumWithdrawn: 0n,
  realizedPnl: 0n,
  unrealizedPnl: 0n,
  firstDepositTimestamp: ts,
  updatedAtTimestamp: ts,
});

indexer.onEvent({ contract: "SweemVault", event: "Deposit" }, async ({ event, context }) => {
  const assets = usdcToWad(event.params.assets);
  const shares = sharesToWad(event.params.shares);
  const owner = event.params.owner;
  const ts = BigInt(event.block.timestamp);

  const before = await context.LpAccount.get(owner);
  const lp = before ?? EMPTY_LP(owner, ts);

  // bump lpCount before the delta writes the snapshot
  if (!before) {
    const current = await getVault(context);
    context.Vault.set({ ...current, lpCount: current.lpCount + 1 });
  }

  // delta first: the account is then valued at the share price this deposit
  // produced, not the one before it
  const vault = await applyVaultDelta(context, event, {
    kind: "DEPOSIT",
    assetsDelta: assets,
    sharesDelta: shares,
  });

  const next = {
    ...lp,
    shares: lp.shares + shares,
    costBasis: lp.costBasis + assets,
    cumDeposited: lp.cumDeposited + assets,
    updatedAtTimestamp: ts,
  };
  context.LpAccount.set({ ...next, unrealizedPnl: unrealizedPnlOf(next, vault.sharePrice) });

  const flow: VaultFlow = {
    id: eventId(event.block.number, event.logIndex),
    account_id: owner,
    kind: "DEPOSIT",
    assets,
    shares,
    sharePrice: vault.sharePrice,
    realizedPnl: 0n,
    caller: event.params.sender,
    counterparty: owner,
    timestamp: ts,
    blockNumber: BigInt(event.block.number),
    txHash: event.transaction.hash,
  };
  context.VaultFlow.set(flow);
});

indexer.onEvent({ contract: "SweemVault", event: "Withdraw" }, async ({ event, context }) => {
  const assets = usdcToWad(event.params.assets);
  const shares = sharesToWad(event.params.shares);
  const owner = event.params.owner;
  const ts = BigInt(event.block.timestamp);

  const lp = (await context.LpAccount.get(owner)) ?? EMPTY_LP(owner, ts);

  const fraction = lp.shares === 0n ? 0n : divWad(shares, lp.shares);
  const basisOut = mulWad(lp.costBasis, fraction);
  const realized = assets - basisOut;

  const vault = await applyVaultDelta(context, event, {
    kind: "WITHDRAW",
    assetsDelta: -assets,
    sharesDelta: -shares,
  });

  const next = {
    ...lp,
    shares: lp.shares - shares,
    costBasis: lp.costBasis - basisOut,
    cumWithdrawn: lp.cumWithdrawn + assets,
    realizedPnl: lp.realizedPnl + realized,
    updatedAtTimestamp: ts,
  };
  context.LpAccount.set({ ...next, unrealizedPnl: unrealizedPnlOf(next, vault.sharePrice) });

  const flow: VaultFlow = {
    id: eventId(event.block.number, event.logIndex),
    account_id: owner,
    kind: "WITHDRAW",
    assets,
    shares,
    // priced pre-withdrawal, which is what the LP actually got
    sharePrice: sharePriceOf(vault.totalAssets + assets, vault.totalShares + shares),
    realizedPnl: realized,
    caller: event.params.sender,
    counterparty: event.params.receiver,
    timestamp: ts,
    blockNumber: BigInt(event.block.number),
    txHash: event.transaction.hash,
  };
  context.VaultFlow.set(flow);
});
