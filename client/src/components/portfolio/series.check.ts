// bun run src/components/portfolio/series.check.ts
import assert from "node:assert/strict";
import { dummySeries } from "./series";

const a = dummySeries("0xAbC", "1W", 123.45, 1e12);
assert.deepEqual(a, dummySeries("0xabc", "1W", 123.45, 1e12));
assert.notDeepEqual(a, dummySeries("0xabd", "1W", 123.45, 1e12));
assert.ok(Math.abs(a.at(-1)!.v - 123.45) < 1e-9 && a.at(-1)!.t === 1e12);
assert.ok(a.every((p) => p.v > 0) && new Set(a.map((p) => p.v)).size > 50);
assert.ok(dummySeries("0xabc", "1D", 0, 0).every((p) => p.v === 0));
console.log("series ok");
