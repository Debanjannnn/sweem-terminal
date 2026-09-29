"use client";

import { useState } from "react";
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { fmtUsd, getNavHistory } from "@/lib/api";
import { useAsync } from "@/lib/session";
import { ErrorBox, Skeleton } from "@/components/ui";
import { ChangeText } from "@/components/invest/BasketCard";
import { cn } from "@/lib/utils";

const D = 86400e3;
const RANGES = { "1D": D, "1W": 7 * D, "1M": 30 * D, "3M": 90 * D, "1Y": 365 * D, ALL: 0 } as const;
type Range = keyof typeof RANGES;

// Price card: live NAV headline + range-scoped change + area chart; tint follows direction.
export function NavChart({ basket, price }: { basket: string; price: number | null }) {
  const [range, setRange] = useState<Range>("1D");
  const hist = useAsync(`nav-hist:${basket}:${range}`, () =>
    getNavHistory(basket, new Date(RANGES[range] ? Date.now() - RANGES[range] : 0), new Date()),
  );
  const points = (hist.data ?? []).map((p) => ({ t: new Date(p.ts).getTime(), nav: Number(p.navPerShare) }));
  const first = points[0]?.nav;
  const last = price ?? points.at(-1)?.nav;
  const change = points.length > 1 && first && last ? (last / first - 1) * 100 : null;
  const delta = change !== null && first && last ? last - first : null;
  const down = change !== null && Number(change.toFixed(2)) < 0;
  const color = down ? "var(--destructive)" : "var(--primary)";
  const fmtT = (t: number) =>
    new Date(t).toLocaleString("en-US", range === "1D" ? { hour: "2-digit", minute: "2-digit" } : { month: "2-digit", day: "2-digit" });

  return (
    <section className={cn("rounded-[32px] p-6", down ? "bg-destructive/[0.07]" : "bg-primary/[0.06]")}>
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <div className="tnum text-5xl font-medium tracking-tight">
            {last !== undefined && last !== null ? fmtUsd(last) : <Skeleton className="h-12 w-48" />}
          </div>
          <div className="mt-2 text-sm">
            {change !== null && delta !== null ? (
              <span className="inline-flex items-center gap-1.5">
                <ChangeText value={change} className="gap-1.5" />
                <span className={cn("font-mono", down ? "text-destructive" : "text-positive")}>
                  ({delta >= 0 ? "+" : "-"}
                  {fmtUsd(Math.abs(delta))}) {range}
                </span>
              </span>
            ) : (
              <span className="text-muted-foreground">NAV per share</span>
            )}
          </div>
        </div>
        <div role="tablist" aria-label="Chart range" className="flex rounded-lg bg-foreground/[0.05] p-0.5">
          {(Object.keys(RANGES) as Range[]).map((r) => (
            <button
              key={r}
              role="tab"
              aria-selected={r === range}
              onClick={() => setRange(r)}
              className={cn(
                "rounded-md px-2.5 py-1 text-sm font-medium transition-colors",
                r === range ? "bg-card text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground",
              )}
            >
              {r}
            </button>
          ))}
        </div>
      </div>
      <div className="mt-6 h-72">
        {hist.error ? (
          <ErrorBox message={hist.error} />
        ) : hist.loading && !hist.data ? (
          <Skeleton className="h-full bg-foreground/[0.04]" />
        ) : points.length === 0 ? (
          <div className="grid h-full place-items-center text-sm text-muted-foreground">No NAV points in this range yet.</div>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={points} margin={{ top: 4, right: 0, bottom: 0, left: 0 }}>
              <defs>
                <linearGradient id="navFill" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor={color} stopOpacity={0.22} />
                  <stop offset="100%" stopColor={color} stopOpacity={0} />
                </linearGradient>
              </defs>
              <CartesianGrid vertical={false} strokeDasharray="3 4" stroke="var(--border)" />
              <XAxis
                dataKey="t"
                type="number"
                scale="time"
                domain={["dataMin", "dataMax"]}
                tickFormatter={fmtT}
                tick={{ fill: "var(--muted-foreground)", fontSize: 10 }}
                axisLine={false}
                tickLine={false}
                minTickGap={48}
              />
              <YAxis
                orientation="right"
                domain={["auto", "auto"]}
                tickFormatter={(v: number) => `$${v.toFixed(v < 10 ? 4 : 2)}`}
                tick={{ fill: "var(--muted-foreground)", fontSize: 10 }}
                axisLine={false}
                tickLine={false}
                width={64}
              />
              <Tooltip
                cursor={{ stroke: "var(--muted-foreground)", strokeDasharray: "3 3" }}
                contentStyle={{
                  background: "var(--popover)",
                  border: "1px solid var(--border)",
                  borderRadius: 12,
                  fontSize: 12,
                }}
                labelFormatter={(t) => new Date(Number(t)).toLocaleString()}
                formatter={(v) => [fmtUsd(Number(v)), "NAV/share"]}
              />
              <Area type="linear" dataKey="nav" stroke={color} strokeWidth={1.75} fill="url(#navFill)" isAnimationActive={false} />
            </AreaChart>
          </ResponsiveContainer>
        )}
      </div>
    </section>
  );
}
