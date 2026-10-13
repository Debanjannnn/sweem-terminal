// bun run src/components/earn/basketIndex.check.ts
import assert from "node:assert/strict";
import { basketCandles } from "./basketIndex";
import type { Candle } from "@/lib/types";

const bar = (t: number, o: number, h: number, l: number, c: number, q = 10): Candle => ({
  market: "x", interval: "1h", t, o: String(o), h: String(h), l: String(l), c: String(c), v: "0", q: String(q), n: 1,
});

// 50% WETH (2000 → 2200), 50% USDC ($1): price = 0.5 × 1 + 0.5 × ETH
const pts = basketCandles([
  { weightBps: 5000, candles: [bar(1, 1990, 2010, 1980, 2000), bar(2, 2000, 2250, 1990, 2200)] },
  { weightBps: 5000, candles: null },
]);
assert.equal(pts.length, 2);
assert.equal(pts[0].v, 1000.5);
assert.equal(pts[1].v, 1100.5);
assert.equal(pts[1].h, 0.5 + 0.5 * 2250);
assert.equal(pts[1].vol, 10);

// starts at the later first bar; a leg missing a bar holds its last close
const two = basketCandles([
  { weightBps: 5000, candles: [bar(1, 1, 1, 1, 1), bar(2, 1, 1, 1, 2), bar(3, 2, 2, 2, 4)] },
  { weightBps: 5000, candles: [bar(2, 10, 10, 10, 10)] },
]);
assert.deepEqual(two.map((p) => p.t), [2, 3]);
assert.equal(two[1].v, 0.5 * 4 + 0.5 * 10); // leg B held at 10

assert.deepEqual(basketCandles([{ weightBps: 10000, candles: [] }]), []);
assert.deepEqual(basketCandles([{ weightBps: 10000, candles: null }]), []);
console.log("basketIndex ok");
