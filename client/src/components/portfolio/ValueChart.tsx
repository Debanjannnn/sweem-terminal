"use client";

import { useState } from "react";
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { Triangle } from "lucide-react";
import { fmtPctSigned, fmtUsd, getCandles } from "@/lib/api";
import { useAsync } from "@/lib/session";
import { marketOf } from "@/lib/tokens";
import { Panel, Skeleton } from "@/components/ui";
import { cn } from "@/lib/utils";
import { Flash, TabPill } from "@/components/motion";
import { holdingsSeries, RANGES, type Range } from "./series";

const usd2 = (n: number) => n.toLocaleString("en-US", { style: "currency", currency: "USD", minimumFractionDigits: 2, maximumFractionDigits: 2 });

// legs = held tokens with a Kuru market (symbol = market); flat = USD that doesn't move with price (stables, principal)
export function ValueChart({ legs, flat, total, loading }: { legs: { market: string; amount: number }[]; flat: number; total: number; loading: boolean }) {
  const [range, setRange] = useState<Range>("1D");
  const { iv, ms } = RANGES[range];
  const legKey = legs.map((l) => `${l.market}:${l.amount}`).join();
  const hist = useAsync(loading ? null : `pf:${legKey}:${range}`, async () => {
    const now = Date.now();
    const candles = await Promise.all(legs.map((l) => getCandles(marketOf(l.market)!.orderbook, iv, ms === null ? undefined : now - ms)));
    return { now, candles };
  });
  const series = hist.data ? holdingsSeries(legs.map((l, i) => ({ amount: l.amount, candles: hist.data!.candles[i] })), flat, hist.data.now, total) : [];
  // nothing price-sensitive held: value is flat over the range
  const points = series.length === 1 && legs.length === 0 ? [{ t: series[0].t - (ms ?? 30 * 86400e3), v: total }, ...series] : series;
  const first = points[0]?.v ?? total;
  const diff = total - first;
  const up = diff >= 0;
  const [whole, cents] = usd2(total).split(".");
  const fmtT = (t: number) =>
    new Date(t).toLocaleString("en-US", range === "1D" ? { hour: "numeric", minute: "2-digit" } : { day: "numeric", month: "short" });
  const tone = up ? "var(--positive)" : "var(--destructive)";

  return (
    <Panel className="p-5 sm:p-6">
      <div className="flex flex-wrap items-center justify-between gap-4">
        {loading ? (
          <Skeleton className="h-12 w-48" />
        ) : (
          <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
            <Flash value={total} className="tnum text-4xl font-medium tracking-tight sm:text-5xl">
              {whole}
              <span className="text-muted-foreground/60">.{cents}</span>
            </Flash>
            <span className={cn("inline-flex items-center gap-1 font-mono text-sm", up ? "text-positive" : "text-destructive")}>
              <Triangle className={cn("size-2.5 fill-current", !up && "rotate-180")} />
              {usd2(Math.abs(diff))} ({fmtPctSigned(first > 0 ? (diff / first) * 100 : 0).replace(/^[+-]/, "")}) {range}
            </span>
          </div>
        )}
        <div role="tablist" className="flex rounded-lg border border-border bg-secondary/30 p-0.5">
          {(Object.keys(RANGES) as Range[]).map((r) => (
            <button
              key={r}
              role="tab"
              aria-selected={r === range}
              onClick={() => setRange(r)}
              className={cn(
                "relative rounded-md px-2.5 py-1 text-xs font-medium transition-colors sm:px-3",
                r === range ? "text-foreground" : "text-muted-foreground hover:text-foreground",
              )}
            >
              {r === range && <TabPill id="pf-range" className="rounded-md bg-card shadow-sm" />}
              <span className="relative">{r}</span>
            </button>
          ))}
        </div>
      </div>
      <div className="mt-6 h-56 sm:h-64">
        {loading || (hist.loading && !hist.data) ? (
          <Skeleton className="h-full" />
        ) : hist.error ? (
          <div className="grid h-full place-items-center text-sm text-muted-foreground">Price history unavailable: {hist.error}</div>
        ) : points.length < 2 ? (
          <div className="grid h-full place-items-center text-sm text-muted-foreground">No price history for your holdings in this range yet.</div>
        ) : (
          <ResponsiveContainer width="100%" height="100%" initialDimension={{ width: 1, height: 1 }}>
            <AreaChart data={points} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
              <defs>
                <linearGradient id="pfFill" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor={tone} stopOpacity={0.2} />
                  <stop offset="100%" stopColor={tone} stopOpacity={0} />
                </linearGradient>
              </defs>
              <CartesianGrid vertical={false} stroke="var(--border)" strokeDasharray="3 3" />
              <XAxis
                dataKey="t"
                type="number"
                scale="time"
                domain={["dataMin", "dataMax"]}
                tickFormatter={fmtT}
                tick={{ fill: "var(--muted-foreground)", fontSize: 10 }}
                axisLine={false}
                tickLine={false}
                minTickGap={40}
              />
              <YAxis
                domain={["auto", "auto"]}
                tickFormatter={(v: number) => fmtUsd(v)}
                tick={{ fill: "var(--muted-foreground)", fontSize: 10 }}
                axisLine={false}
                tickLine={false}
                width={60}
              />
              <Tooltip
                contentStyle={{ background: "var(--popover)", border: "1px solid var(--border)", borderRadius: 12, fontSize: 12 }}
                labelFormatter={(t) => new Date(Number(t)).toLocaleString()}
                formatter={(v) => [usd2(Number(v)), "Value"]}
              />
              <Area type="linear" dataKey="v" stroke={tone} strokeWidth={1.5} fill="url(#pfFill)" isAnimationActive={false} />
            </AreaChart>
          </ResponsiveContainer>
        )}
      </div>
      <p className="mt-2 text-[11px] text-muted-foreground">Your current holdings valued at past Kuru prices; the last point is live.</p>
    </Panel>
  );
}
