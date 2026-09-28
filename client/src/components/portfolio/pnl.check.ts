// bun run src/components/portfolio/pnl.check.ts
import assert from "node:assert/strict";
import { pnlByBasket } from "./pnl";

const t = (kind: "mint" | "redeem", basket: string, usdcRaw: string) =>
  ({ kind, basket, usdcRaw, txHash: "0x", logIndex: 0, sharesRaw: "0", block: 0, ts: "" }) as const;
const m = pnlByBasket(
  [t("mint", "a", "100000000"), t("redeem", "a", "30000000"), t("mint", "b", "50000000"), t("redeem", "b", "55000000")],
  [{ basket: "a", sharesRaw: "1", navPerShare: "1", valueUsdc: "80.1234567", navTs: null }],
);
assert.equal(m.get("a")!.costRaw, BigInt(70_000_000));
assert.equal(m.get("a")!.pnlRaw, BigInt(10_123_457));
assert.equal(m.get("b")!.pnlRaw, BigInt(5_000_000));
console.log("pnl ok");
