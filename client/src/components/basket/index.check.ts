// bun run src/components/basket/index.check.ts
import assert from "node:assert/strict";
import type { Candle } from "@/lib/types";
import { basketIndex } from "./index";

const c = (t: number, px: number) => ({ t, c: String(px) }) as Candle;
const out = basketIndex([
  { weightBps: 5000, candles: [c(0, 10), c(1, 20), c(2, 20)] },
  { weightBps: 5000, candles: [c(1, 100), c(2, 50)] },
]);
assert.deepEqual(out, [
  { t: 1, v: 100 },
  { t: 2, v: 75 },
]);
assert.deepEqual(basketIndex([{ weightBps: 10000, candles: [] }]), []);
console.log("ok");
