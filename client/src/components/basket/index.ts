import type { Candle } from "@/lib/types";

// Value of $100 split by weight at the first common candle, marked at each later close (legs forward-filled).
export function basketIndex(legs: { weightBps: number; candles: Candle[] }[]): { t: number; v: number }[] {
  if (!legs.length || legs.some((l) => !l.candles.length)) return [];
  const start = Math.max(...legs.map((l) => l.candles[0].t));
  const times = [...new Set(legs.flatMap((l) => l.candles.map((c) => c.t)))].filter((t) => t >= start).sort((a, b) => a - b);
  const cursors = legs.map(() => 0);
  const last = legs.map(() => 0);
  const base = legs.map(() => 0);
  return times.map((t) => {
    let v = 0;
    legs.forEach((l, i) => {
      while (cursors[i] < l.candles.length && l.candles[cursors[i]].t <= t) last[i] = Number(l.candles[cursors[i]++].c);
      if (!base[i]) base[i] = last[i];
      v += base[i] ? (l.weightBps / 100) * (last[i] / base[i]) : 0;
    });
    return { t, v };
  });
}
