"use client";

import { Cell, Legend, Line, LineChart, Pie, PieChart, ResponsiveContainer, Tooltip, XAxis, YAxis, CartesianGrid } from "recharts";
import { errText, fmtUsd } from "@/lib/api";
import type { Backtest, Source } from "@/lib/types";
import { ErrorBox, Notice, Skeleton, Stat } from "@/components/ui";
import { COLORS } from "@/components/create/CreateBasket";
import { KIND_LABEL, venueName } from "./sources";

export const pct = (s: string | number | null | undefined, d = 2) =>
  s === null || s === undefined || s === "" ? "—" : `${(Number(s) * 100).toFixed(d)}%`;

// equity vs HODL over the backtest window + headline stats + coverage notes
export function BacktestView({ bt, loading, error }: { bt: Backtest | null; loading: boolean; error: string | null }) {
  if (error) return <ErrorBox message={errText(new Error(error))} />;
  if (!bt) return loading ? <Skeleton className="h-80 w-full" /> : <p className="text-sm text-muted-foreground">Run a backtest to see history.</p>;
  const data = bt.curve.map((p) => ({ day: p.day, usd: Number(p.usd), hodl: Number(p.hodlUsd) }));
  const clamped = bt.coverage.filter((c) => c.note);
  return (
    <div className="space-y-4">
      <dl className="grid grid-cols-2 gap-6 md:grid-cols-4">
        <Stat label="APY (USD)" value={<span className="text-positive">{pct(bt.apyUsd)}</span>} note={`asset ${pct(bt.apyAsset)}`} />
        <Stat label="Max drawdown" value={pct(bt.maxDrawdownUsd)} note={`HODL ${pct(bt.hodl.maxDrawdownUsd)}`} />
        <Stat label="HODL APY" value={pct(bt.hodl.apyUsd)} />
        <Stat label="Final" value={fmtUsd(Number(bt.finalUsd))} note={`from ${fmtUsd(Number(bt.initialUsd))} · ${bt.days}d`} />
      </dl>
      <div className="h-64">
        <ResponsiveContainer width="100%" height="100%" initialDimension={{ width: 1, height: 1 }}>
          <LineChart data={data} margin={{ top: 4, right: 0, bottom: 0, left: 0 }}>
            <CartesianGrid vertical={false} strokeDasharray="3 4" stroke="var(--border)" />
            <XAxis dataKey="day" tick={{ fill: "var(--muted-foreground)", fontSize: 10 }} axisLine={false} tickLine={false} minTickGap={48} />
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
              contentStyle={{ background: "var(--popover)", border: "1px solid var(--border)", borderRadius: 12, fontSize: 12 }}
              formatter={(v, n) => [fmtUsd(Number(v)), n === "usd" ? "Strategy" : "HODL"]}
            />
            <Legend formatter={(v) => (v === "usd" ? "Strategy" : "HODL")} wrapperStyle={{ fontSize: 12 }} />
            <Line type="linear" dataKey="usd" stroke="var(--primary)" strokeWidth={1.75} dot={false} isAnimationActive={false} />
            <Line type="linear" dataKey="hodl" stroke="var(--muted-foreground)" strokeWidth={1.25} strokeDasharray="4 3" dot={false} isAnimationActive={false} />
          </LineChart>
        </ResponsiveContainer>
      </div>
      <p className="text-xs text-muted-foreground">
        {bt.label} · {bt.from} → {bt.to} · {bt.rebalances} rebalances · costs {fmtUsd(Number(bt.costsUsd))}
      </p>
      {bt.estimated && <Notice>Includes the Kuru ActiveVault leg, which is ESTIMATED (assumed APR, no venue history).</Notice>}
      {clamped.length > 0 && (
        <Notice tone="muted">
          Coverage: {clamped.map((c) => `${c.series.split(":").slice(1, 3).join(" ")} from ${c.first} (${c.note})`).join("; ")}
        </Notice>
      )}
    </div>
  );
}

export function AllocationDonut({ sources }: { sources: Source[] }) {
  const data = sources.map((s) => ({ name: `${KIND_LABEL[s.kind]} · ${venueName(s.venue)}`, value: s.weightBps }));
  return (
    <div className="flex flex-wrap items-center gap-6">
      <div className="size-40">
        <ResponsiveContainer width="100%" height="100%" initialDimension={{ width: 1, height: 1 }}>
          <PieChart>
            <Pie data={data} dataKey="value" innerRadius="62%" outerRadius="100%" stroke="var(--card)" strokeWidth={2} isAnimationActive={false}>
              {data.map((_, i) => (
                <Cell key={i} fill={COLORS[i % COLORS.length]} />
              ))}
            </Pie>
          </PieChart>
        </ResponsiveContainer>
      </div>
      <ul className="min-w-40 flex-1 space-y-2 text-sm">
        {data.map((d, i) => (
          <li key={d.name} className="flex items-center gap-2">
            <span className="size-2.5 shrink-0 rounded-full" style={{ background: COLORS[i % COLORS.length] }} />
            <span className="truncate">{d.name}</span>
            <span className="tnum ml-auto text-muted-foreground">{d.value / 100}%</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
