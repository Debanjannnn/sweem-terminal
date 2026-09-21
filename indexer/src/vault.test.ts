import { describe, expect, it } from "vitest";
import { applyVaultDelta, type VaultDelta } from "./vault.js";
import { WAD } from "./scale.js";
import { VAULT_ID } from "./ids.js";

// in-memory stand-in for the envio context: enough of it to drive the reducer
function fakeContext(lpAccounts: Record<string, unknown> = {}) {
  const store: Record<string, Record<string, any>> = { Vault: {}, Protocol: {}, LpAccount: { ...lpAccounts } };
  const table = (name: string) => ({
    getOrCreate: async (e: any) => store[name]![e.id] ?? e,
    get: async (id: string) => store[name]![id],
    set: (e: any) => {
      store[name]![e.id] = e;
    },
    getWhere: async () => Object.values(store[name]!),
  });
  return { store, ctx: { Vault: table("Vault"), Protocol: table("Protocol"), LpAccount: table("LpAccount") } };
}

const event = { block: { number: 1, timestamp: 1_700_000_000 } };

const run = async (ctx: any, deltas: VaultDelta[]) => {
  let vault: any;
  for (const d of deltas) vault = await applyVaultDelta(ctx, event, d);
  return vault;
};

describe("applyVaultDelta", () => {
  it("prices shares and books each income leg to its own bucket", async () => {
    const { ctx } = fakeContext();
    const vault = await run(ctx, [
      { kind: "DEPOSIT", assetsDelta: 1000n * WAD, sharesDelta: 1000n * WAD },
      { kind: "BORROW_ACCRUAL", assetsDelta: 50n * WAD },
      { kind: "FUNDING_ACCRUAL", assetsDelta: 20n * WAD },
      { kind: "TRADING_FEE", assetsDelta: 10n * WAD },
      { kind: "LIQUIDATION_FEE", assetsDelta: 5n * WAD },
      { kind: "TRADER_PNL", assetsDelta: -30n * WAD },
      { kind: "BAD_DEBT", assetsDelta: -5n * WAD },
    ]);

    expect(vault.totalAssets).toBe(1050n * WAD);
    expect(vault.totalShares).toBe(1000n * WAD);
    expect(vault.sharePrice).toBe((105n * WAD) / 100n);
    expect(vault.cumBorrowIncome).toBe(50n * WAD);
    expect(vault.cumFundingIncome).toBe(20n * WAD);
    expect(vault.cumTradingFeeIncome).toBe(10n * WAD);
    expect(vault.cumLiquidationFeeIncome).toBe(5n * WAD);
    expect(vault.cumTraderPnlPaid).toBe(30n * WAD);
    expect(vault.cumBadDebt).toBe(5n * WAD);

    // the decomposition has to explain the share price, or LP APY is fiction
    const income =
      vault.cumBorrowIncome +
      vault.cumFundingIncome +
      vault.cumTradingFeeIncome +
      vault.cumLiquidationFeeIncome -
      vault.cumTraderPnlPaid -
      vault.cumBadDebt;
    expect(vault.totalAssets - vault.cumDeposited + vault.cumWithdrawn).toBe(income);
  });

  it("revalues the whole LP book when share price moves", async () => {
    const lp = {
      id: "0xlp",
      vault_id: VAULT_ID,
      shares: 1000n * WAD,
      costBasis: 1000n * WAD,
      unrealizedPnl: 0n,
    };
    const { store, ctx } = fakeContext({ "0xlp": lp });
    await run(ctx, [
      { kind: "DEPOSIT", assetsDelta: 1000n * WAD, sharesDelta: 1000n * WAD },
      { kind: "BORROW_ACCRUAL", assetsDelta: 100n * WAD },
    ]);
    expect(store.LpAccount!["0xlp"]!.unrealizedPnl).toBe(100n * WAD);
  });
});
