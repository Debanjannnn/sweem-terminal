// bun run src/components/earn/sources.check.ts
import assert from "node:assert/strict";
import { blendedApy, kindsFor, lendingVenues, minDeposit, parts, toBacktest } from "./sources";
import type { Rates } from "@/lib/types";

// MON lends as WMON
assert.deepEqual(
  lendingVenues("MON").map((v) => v.venue),
  ["neverland", "euler-k3wmon"],
);
assert.ok(lendingVenues("USDC").some((v) => v.venue === "euler-k3usdc"));
assert.ok(lendingVenues("USDC").some((v) => v.venue === "morpho-hyperusdca"));
assert.deepEqual(kindsFor("MON"), ["lending", "staking", "lp", "basis"]);
assert.deepEqual(kindsFor("XAUt0"), ["lending"]);
const src = [
  { kind: "lending" as const, venue: "aave", weightBps: 5000 },
  { kind: "basis" as const, venue: "perpl", weightBps: 5000, params: { leverage: "3" } },
];
const weth = parts({ asset: "WETH", sources: src });
assert.deepEqual(weth, [{ asset: "WETH", weightBps: 10000, sources: src }]);
const bt = toBacktest(weth, { fundingGate: { apr: "0.05", lookbackDays: 7 } }, "1");
assert.equal(bt.asset, "WETH");
assert.deepEqual(bt.legs?.[1], { kind: "basis", perpId: 20, weight: "0.5" });
assert.equal(bt.rules?.basisLeverage, "3");
const rates = {
  ts: 0,
  stale: false,
  rates: [
    { source: "lending", venue: "aave", asset: "WETH", address: "", apy: "0.02", kind: "supply", ts: 0 },
    { source: "lending", venue: "aave", asset: "USDC", address: "", apy: "0.05", kind: "supply", ts: 0 },
  ],
  funding: [{ perpId: 20, name: "ETH", rate: "0", intervalSec: 3600, aprShort: "0.1", aprShort7d: "0.08", block: 0, ts: 0 }],
} satisfies Rates;
// 0.5×0.02 + 0.5×(0.08×3/4)
assert.ok(Math.abs(blendedApy(weth, rates)! - 0.04) < 1e-12);

// multi: 40% USDC aave + 60% WETH (above)
const multi = [{ asset: "USDC", weightBps: 4000, sources: [{ kind: "lending" as const, venue: "aave", weightBps: 10000 }] }, { ...weth[0], weightBps: 6000 }];
assert.ok(Math.abs(blendedApy(multi, rates)! - (0.4 * 0.05 + 0.6 * 0.04)) < 1e-12);
const mb = toBacktest(multi, { driftBps: 300 }, "1000");
assert.equal(mb.asset, undefined);
assert.equal(mb.amount, "1000");
assert.equal(mb.sleeves?.length, 2);
assert.equal(mb.sleeves?.[1].weight, "0.6");
assert.equal(mb.sleeves?.[1].rules?.basisLeverage, "3");
assert.equal(mb.rules?.driftBps, "300");

// WETH min 0.002; buyShare 0.5×1 + 0.5×3/4 = 0.875; sleeve 60%, fee 0.4%
const md = minDeposit(multi, { WETH: 2000 });
const want = (0.002 * 2000 * 1.05) / (0.875 * 0.6 * 0.996);
assert.equal(md.per.USDC, 0);
assert.ok(Math.abs(md.per.WETH! - want) < 1e-9);
assert.ok(Math.abs(md.total! - want) < 1e-9);
assert.equal(minDeposit(multi, {}).total, null);
// lp buys half the MON
const lp = minDeposit([{ asset: "MON", weightBps: 10000, sources: [{ kind: "lp", venue: "kuru-activevault", weightBps: 10000 }] }], { MON: 0.05 });
assert.ok(Math.abs(lp.total! - (200 * 0.05 * 1.05) / (0.5 * 0.996)) < 1e-9);
console.log("ok");
