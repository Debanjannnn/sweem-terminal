// bun run src/components/portfolio/series.check.ts
import assert from "node:assert/strict";
import type { Candle } from "@/lib/types";
import { holdingsSeries } from "./series";

const c = (t: number, o: number, cl: number) => ({ t, o: String(o), c: String(cl) }) as Candle;
const s = holdingsSeries([{ amount: 2, candles: [c(10, 5, 6), c(30, 6, 8)] }, { amount: 1, candles: [c(20, 100, 110)] }], 50, 40, 999);
// t10: 50 + 2*6 + 1*100(backfilled open); t20: 50+12+110; t30: 50+16+110; then live
assert.deepEqual(s, [{ t: 10, v: 162 }, { t: 20, v: 172 }, { t: 30, v: 176 }, { t: 40, v: 999 }]);
assert.deepEqual(holdingsSeries([], 7, 5, 7), [{ t: 5, v: 7 }]);
console.log("series ok");
