"use client";

import { useState } from "react";
import { Area, AreaChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { fmtPctSigned, fmtUsd, getNavHistory } from "@/lib/api";
import { useAsync } from "@/lib/session";
import { ErrorBox, Label, Panel, Skeleton } from "@/components/ui";
import { Segmented } from "@/components/motion";
import { cn } from "@/lib/utils";

const RANGES = { "1H": 3600e3, "1D": 86400e3, "1W": 7 * 86400e3, ALL: 0 } as const;
type Range = keyof typeof RANGES;

export function NavChart({ basket }: { basket: string }) {
  const [range, setRange] = useState<Range>("1D");
  const hist = useAsync(`nav-hist:${basket}:${range}`, () =>
    getNavHistory(basket, new Date(RANGES[range] ? Date.now() - RANGES[range] : 0), new Date()),
  );
  const points = (hist.data ?? []).map((p) => ({ t: new Date(p.ts).getTime(), nav: Number(p.navPerShare) }));
  const change = points.length > 1 ? (points.at(-1)!.nav / points[0].nav - 1) * 100 : null;
  const fmtT = (t: number) =>
    new Date(t).toLocaleString("en-US", range === "1H" || range === "1D" ? { hour: "numeric", minute: "2-digit" } : { month: "short", day: "numeric" });

  return (
    <Panel>
      <div className="flex flex-wrap items-center justify-between gap-3 px-5 pt-5">
        <div className="flex items-baseline gap-3">
          <Label>NAV per share</Label>
          {change !== null && (
            <span className={cn("tnum text-sm", change >= 0 ? "text-positive" : "text-destructive")}>
              {fmtPctSigned(change)}
            </span>
          )}
        </div>
        <div className="w-60">
          <Segmented
            id="nav-range"
            value={range}
            onChange={setRange}
            options={(Object.keys(RANGES) as Range[]).map((r) => ({ value: r, label: r }))}
          />
        </div>
      </div>
      <div className="h-64 px-2 pb-4 pt-4">
        {hist.error ? (
          <div className="px-3">
            <ErrorBox message={hist.error} />
          </div>
        ) : hist.loading && !hist.data ? (
          <Skeleton className="mx-3 h-full" />
        ) : points.length === 0 ? (
          <div className="grid h-full place-items-center text-sm text-muted-foreground">
            No NAV points in this range yet.
          </div>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={points} margin={{ top: 8, right: 12, bottom: 0, left: 0 }}>
              <defs>
                <linearGradient id="navFill" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor="var(--primary)" stopOpacity={0.25} />
                  <stop offset="100%" stopColor="var(--primary)" stopOpacity={0} />
                </linearGradient>
              </defs>
              <XAxis
                dataKey="t"
                type="number"
                scale="time"
                domain={["dataMin", "dataMax"]}
                tickFormatter={fmtT}
                tick={{ fill: "var(--muted-foreground)", fontSize: 11 }}
                axisLine={false}
                tickLine={false}
                minTickGap={40}
              />
              <YAxis
                domain={["auto", "auto"]}
                tickFormatter={(v: number) => v.toFixed(4)}
                tick={{ fill: "var(--muted-foreground)", fontSize: 11 }}
                axisLine={false}
                tickLine={false}
                width={56}
              />
              <Tooltip
                contentStyle={{
                  background: "var(--popover)",
                  border: "1px solid var(--border)",
                  borderRadius: 12,
                  fontSize: 12,
                }}
                labelFormatter={(t) => new Date(Number(t)).toLocaleString()}
                formatter={(v) => [fmtUsd(Number(v)), "NAV/share"]}
              />
              <Area
                type="monotone"
                dataKey="nav"
                stroke="var(--primary)"
                strokeWidth={2}
                fill="url(#navFill)"
                isAnimationActive={false}
              />
            </AreaChart>
          </ResponsiveContainer>
        )}
      </div>
    </Panel>
  );
}
