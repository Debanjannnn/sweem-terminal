import type { Candle } from "@/lib/types";
import type { Point } from "@/components/chart/PriceChart";

// Basket price = Σ weight × token price (USDC = $1), as OHLC from each token's Kuru candles, from the first common bar.
// h/l add up per-leg extremes (a bound: legs need not peak in the same bar). A leg without a bar at t holds its last close;
// candles null = flat leg (USDC). Volume = Σ legs' USDC volume.
export function basketCandles(legs: { weightBps: number; candles: Candle[] | null }[]): Point[] {
  const live = legs.filter((l) => l.candles);
  if (!live.length || live.some((l) => !l.candles!.length)) return [];
  const flat = legs.filter((l) => !l.candles).reduce((s, l) => s + l.weightBps / 10000, 0);
  const start = Math.max(...live.map((l) => l.candles![0].t));
  const times = [...new Set(live.flatMap((l) => l.candles!.map((c) => c.t)))].filter((t) => t >= start).sort((a, b) => a - b);
  const st = live.map(() => ({ i: 0, last: 0 }));
  return times.map((t) => {
    const p = { t, o: flat, h: flat, l: flat, v: flat, vol: 0 };
    live.forEach((leg, k) => {
      const s = st[k];
      const cs = leg.candles!;
      while (s.i < cs.length && cs[s.i].t < t) s.last = Number(cs[s.i++].c);
      const bar = s.i < cs.length && cs[s.i].t === t ? cs[s.i] : null;
      const [o, h, l, c] = bar ? [bar.o, bar.h, bar.l, bar.c].map(Number) : [s.last, s.last, s.last, s.last];
      const w = leg.weightBps / 10000;
      p.o += w * o;
      p.h += w * h;
      p.l += w * l;
      p.v += w * c;
      p.vol += bar ? Number(bar.q) : 0;
    });
    return p;
  });
}
