import type { Candle, Interval } from "@/lib/types";

const D = 86400e3;
export const RANGES = {
  "1D": { ms: D, iv: "5m" },
  "1W": { ms: 7 * D, iv: "1h" },
  "1M": { ms: 30 * D, iv: "1h" },
  ALL: { ms: null, iv: "1d" },
} as const satisfies Record<string, { ms: number | null; iv: Interval }>;
export type Range = keyof typeof RANGES;

// Today's holdings marked at each past close (legs forward-filled, back-filled with their first open); flat = stables etc.
export function holdingsSeries(legs: { amount: number; candles: Candle[] }[], flat: number, now: number, live: number) {
  const withData = legs.filter((l) => l.candles.length);
  const times = [...new Set(withData.flatMap((l) => l.candles.map((c) => c.t)))].filter((t) => t < now).sort((a, b) => a - b);
  const cur = withData.map(() => 0);
  const px = withData.map((l) => Number(l.candles[0].o));
  const pts = times.map((t) => {
    let v = flat;
    withData.forEach((l, i) => {
      while (cur[i] < l.candles.length && l.candles[cur[i]].t <= t) px[i] = Number(l.candles[cur[i]++].c);
      v += l.amount * px[i];
    });
    return { t, v };
  });
  return [...pts, { t: now, v: live }];
}
