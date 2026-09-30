"use client";

import { Area, Bar, CartesianGrid, ComposedChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { fmtUsd } from "@/lib/api";
import type { Interval } from "@/lib/types";
import { ErrorBox, Skeleton } from "@/components/ui";
import { ChangeText } from "@/components/invest/BasketCard";
import { cn } from "@/lib/utils";
import { Flash, Segmented, TabPill } from "@/components/motion";

const D = 86400e3;
export const RANGES = {
  "1D": { ms: D, iv: "5m" },
  "1W": { ms: 7 * D, iv: "1h" },
  "1M": { ms: 30 * D, iv: "1h" },
  "3M": { ms: 90 * D, iv: "1d" },
  "1Y": { ms: 365 * D, iv: "1d" },
} as const satisfies Record<string, { ms: number; iv: Interval }>;
export type Range = keyof typeof RANGES;

export type Point = { t: number; v: number; o?: number; h?: number; l?: number };

// wick + body inside the [l, h] bar recharts lays out
function CandleShape(p: { x?: number; y?: number; width?: number; height?: number; payload?: Point }) {
  const { x = 0, y = 0, width = 0, height = 0, payload } = p;
  if (!payload || payload.o === undefined || payload.h === undefined || payload.l === undefined) return null;
  const { o, h, l, v: c } = payload;
  const px = (val: number) => (h === l ? y : y + ((h - val) / (h - l)) * height);
  const up = c >= o;
  const color = up ? "var(--positive)" : "var(--destructive)";
  const top = px(Math.max(o, c));
  const bw = Math.max(1, width * 0.7);
  return (
    <g>
      <line x1={x + width / 2} x2={x + width / 2} y1={y} y2={y + height} stroke={color} strokeWidth={1} />
      <rect x={x + (width - bw) / 2} y={top} width={bw} height={Math.max(1, px(Math.min(o, c)) - top)} fill={color} />
    </g>
  );
}

// Price card: live headline + range change + area/candle chart; tint follows direction.
export function PriceChart({
  points,
  price,
  range,
  onRange,
  mode,
  onMode,
  loading,
  error,
  caption,
  empty = "No trades in this range yet.",
}: {
  points: Point[];
  price: number | null;
  range: Range;
  onRange: (r: Range) => void;
  mode?: "line" | "candle";
  onMode?: (m: "line" | "candle") => void;
  loading: boolean;
  error: string | null;
  caption: string;
  empty?: string;
}) {
  const first = points[0]?.o ?? points[0]?.v;
  const last = price ?? points.at(-1)?.v ?? null;
  const change = points.length > 1 && first && last ? (last / first - 1) * 100 : null;
  const down = change !== null && Number(change.toFixed(2)) < 0;
  const color = down ? "var(--destructive)" : "var(--primary)";
  const fmtT = (t: number) =>
    new Date(t).toLocaleString("en-US", range === "1D" ? { hour: "2-digit", minute: "2-digit" } : { month: "short", day: "numeric" });
  const candle = mode === "candle";

  return (
    <section className={cn("rounded-[32px] p-6", down ? "bg-destructive/[0.07]" : "bg-primary/[0.06]")}>
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <div className="tnum text-5xl font-medium tracking-tight">
            {last !== null ? <Flash value={last}>{fmtUsd(last)}</Flash> : loading ? <Skeleton className="h-12 w-48" /> : "—"}
          </div>
          <div className="mt-2 text-sm">
            {change !== null && first && last ? (
              <span className="inline-flex items-center gap-1.5">
                <ChangeText value={change} className="gap-1.5" />
                <span className={cn("font-mono", down ? "text-destructive" : "text-positive")}>
                  ({last - first >= 0 ? "+" : "-"}
                  {fmtUsd(Math.abs(last - first))}) {range}
                </span>
              </span>
            ) : (
              <span className="text-muted-foreground">{caption}</span>
            )}
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {onMode && mode && (
            <Segmented
              id="chart-mode"
              className="w-auto"
              value={mode}
              onChange={onMode}
              options={[
                { value: "line", label: "Line" },
                { value: "candle", label: "Candle" },
              ]}
            />
          )}
          <div role="tablist" aria-label="Chart range" className="flex rounded-lg bg-foreground/[0.05] p-0.5">
            {(Object.keys(RANGES) as Range[]).map((r) => (
              <button
                key={r}
                role="tab"
                aria-selected={r === range}
                onClick={() => onRange(r)}
                className={cn(
                  "relative rounded-md px-2.5 py-1 text-sm font-medium transition-colors",
                  r === range ? "text-foreground" : "text-muted-foreground hover:text-foreground",
                )}
              >
                {r === range && <TabPill id="chart-range" className="rounded-md bg-card shadow-sm" />}
                <span className="relative">{r}</span>
              </button>
            ))}
          </div>
        </div>
      </div>
      <div className="mt-6 h-72">
        {error ? (
          <ErrorBox message={error} />
        ) : loading && points.length === 0 ? (
          <Skeleton className="h-full bg-foreground/[0.04]" />
        ) : points.length === 0 ? (
          <div className="grid h-full place-items-center text-sm text-muted-foreground">{empty}</div>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <ComposedChart data={points} margin={{ top: 4, right: 0, bottom: 0, left: 0 }}>
              <defs>
                <linearGradient id="pxFill" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor={color} stopOpacity={0.22} />
                  <stop offset="100%" stopColor={color} stopOpacity={0} />
                </linearGradient>
              </defs>
              <CartesianGrid vertical={false} strokeDasharray="3 4" stroke="var(--border)" />
              <XAxis
                dataKey="t"
                type={candle ? "category" : "number"}
                scale={candle ? "auto" : "time"}
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
                tickFormatter={(v: number) => fmtUsd(v)}
                tick={{ fill: "var(--muted-foreground)", fontSize: 10 }}
                axisLine={false}
                tickLine={false}
                width={72}
              />
              <Tooltip
                cursor={{ stroke: "var(--muted-foreground)", strokeDasharray: "3 3" }}
                contentStyle={{ background: "var(--popover)", border: "1px solid var(--border)", borderRadius: 12, fontSize: 12 }}
                labelFormatter={(t) => new Date(Number(t)).toLocaleString()}
                formatter={(v, _n, item) => {
                  const p = item.payload as Point;
                  return candle && p.o !== undefined
                    ? [`O ${fmtUsd(p.o)} H ${fmtUsd(p.h!)} L ${fmtUsd(p.l!)} C ${fmtUsd(p.v)}`, ""]
                    : [fmtUsd(Number(Array.isArray(v) ? p.v : v)), caption];
                }}
              />
              {candle ? (
                <Bar dataKey={(p: Point) => [p.l ?? p.v, p.h ?? p.v]} shape={CandleShape} isAnimationActive={false} />
              ) : (
                <Area type="linear" dataKey="v" stroke={color} strokeWidth={1.75} fill="url(#pxFill)" isAnimationActive={false} />
              )}
            </ComposedChart>
          </ResponsiveContainer>
        )}
      </div>
    </section>
  );
}
