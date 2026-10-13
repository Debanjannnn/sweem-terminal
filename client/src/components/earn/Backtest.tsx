"use client";

import { Cell, Line, LineChart, Pie, PieChart, ResponsiveContainer, Tooltip, XAxis, YAxis, CartesianGrid } from "recharts";
import { errText, fmtUsd } from "@/lib/api";
import type { Backtest, Sleeve } from "@/lib/types";
import { ErrorBox, Skeleton, Stat } from "@/components/ui";
import { COLORS } from "@/components/create/CreateBasket";
import { KIND_LABEL, venueName } from "./sources";

export const pct = (s: string | number | null | undefined, d = 2) =>
  s === null || s === undefined || s === "" ? "-" : `${(Number(s) * 100).toFixed(d)}%`;

// "price:cbBTC" | "rate:lending:aave:USDC" | "funding:10"
function seriesLabel(series: string) {
  const [t, a, b, c] = series.split(":");
  if (t === "price") return `${a} price`;
  if (t === "rate") return `${venueName(b)} ${c}`;
  if (t === "funding") return "Perpl funding";
  return series;
}

// equity over the backtest window + headline stats + coverage notes
export function BacktestView({ bt, loading, error }: { bt: Backtest | null; loading: boolean; error: string | null }) {
  if (error) return <ErrorBox message={errText(new Error(error))} />;
  if (!bt) return loading ? <Skeleton className="h-80 w-full" /> : <p className="text-sm text-muted-foreground">Run a backtest to see history.</p>;
  const data = bt.curve.map((p) => ({ day: p.day, usd: Number(p.usd) }));
  // dedupe: multi-token runs repeat the same series per sleeve
  const clamped = [...new Map(bt.coverage.filter((c) => c.note).map((c) => [seriesLabel(c.series), { ...c, label: seriesLabel(c.series) }])).values()];
  return (
    <div className="space-y-4">
      <dl className="grid grid-cols-2 gap-6 md:grid-cols-3">
        <Stat label="APY (USD)" value={<span className="text-positive">{pct(bt.apyUsd)}</span>} note={`asset ${pct(bt.apyAsset)}`} />
        <Stat label="Max drawdown" value={pct(bt.maxDrawdownUsd)} />
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
              formatter={(v) => [fmtUsd(Number(v)), "Strategy"]}
            />
            <Line type="linear" dataKey="usd" stroke="var(--primary)" strokeWidth={1.75} dot={false} isAnimationActive={false} />
          </LineChart>
        </ResponsiveContainer>
      </div>
      <p className="text-xs text-muted-foreground">
        {bt.label} · {bt.from} → {bt.to} · {bt.rebalances} rebalances · costs {fmtUsd(Number(bt.costsUsd))}
      </p>
      {(bt.estimated || clamped.length > 0) && (
        <div className="space-y-3 rounded-xl border border-border bg-secondary/30 p-4 text-xs">
          {bt.estimated && (
            <p className="flex items-center gap-2 text-warning">
              <span className="size-1.5 shrink-0 rounded-full bg-warning" />
              Kuru ActiveVault yield is estimated (assumed APR, no venue history)
            </p>
          )}
          {clamped.length > 0 && (
            <div className="space-y-2">
              <p className="text-muted-foreground">Window shortened to the data every source has</p>
              <div className="flex flex-wrap gap-2">
                {clamped.map((c) => (
                  <span key={c.label} className="tnum inline-flex items-center gap-1.5 rounded-full border border-border bg-card px-2.5 py-1">
                    {c.label}
                    <span className="text-muted-foreground">
                      {c.first} → {c.last}
                    </span>
                  </span>
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// token → source slices; token prefix only when there are several
export function AllocationDonut({ sleeves }: { sleeves: Sleeve[] }) {
  const data = sleeves.flatMap((sl) =>
    sl.sources.map((s) => ({
      name: `${sleeves.length > 1 ? `${sl.asset} · ` : ""}${KIND_LABEL[s.kind]} · ${venueName(s.venue)}`,
      value: (sl.weightBps * s.weightBps) / 10000,
    })),
  );
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
            <span className="tnum ml-auto text-muted-foreground">{+(d.value / 100).toFixed(2)}%</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
