// bun run src/components/earn/sources.check.ts
import assert from "node:assert/strict";
import { blendedApy, kindsFor, lendingVenues, toBacktest } from "./sources";
import type { Rates } from "@/lib/types";

assert.deepEqual(lendingVenues("MON"), []);
assert.ok(lendingVenues("USDC").some((v) => v.venue === "euler-k3usdc"));
assert.ok(lendingVenues("USDC").some((v) => v.venue === "morpho-hyperusdca"));
assert.deepEqual(kindsFor("MON"), ["staking", "lp", "basis"]);
assert.deepEqual(kindsFor("XAUt0"), ["lending"]);
const src = [
  { kind: "lending" as const, venue: "aave", weightBps: 5000 },
  { kind: "basis" as const, venue: "perpl", weightBps: 5000, params: { leverage: "3" } },
];
const bt = toBacktest("WETH", src, { fundingGate: { apr: "0.05", lookbackDays: 7 } }, "1");
assert.deepEqual(bt.legs[1], { kind: "basis", perpId: 20, weight: "0.5" });
assert.equal(bt.rules?.basisLeverage, "3");
const rates = {
  ts: 0,
  stale: false,
  rates: [{ source: "lending", venue: "aave", asset: "WETH", address: "", apy: "0.02", kind: "supply", ts: 0 }],
  funding: [{ perpId: 20, name: "ETH", rate: "0", intervalSec: 3600, aprShort: "0.1", aprShort7d: "0.08", block: 0, ts: 0 }],
} satisfies Rates;
// 0.5×0.02 + 0.5×(0.08×3/4)
assert.ok(Math.abs(blendedApy(src, "WETH", rates)! - 0.04) < 1e-12);
console.log("ok");
