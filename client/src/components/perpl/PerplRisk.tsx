"use client";

import { Bar, BarChart, Cell, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { ChevronDown, ExternalLink } from "lucide-react";
import { fmtUsd, fmtUsdCompact, getFunding, getPerplMarkets, getPositionPerpl } from "@/lib/api";
import { addressUrl, DEPLOYMENTS } from "@/lib/config";
import { useAsync } from "@/lib/session";
import type { FundingDay, PerplMarket, PerplRisk } from "@/lib/types";
import { cn } from "@/lib/utils";
import { ErrorBox, Panel, Skeleton } from "@/components/ui";
import { ProtocolIcon } from "@/components/ProtocolIcon";
import { Sparkline } from "@/components/Sparkline";
import { TokenIcon } from "@/components/TokenIcon";
import { pct } from "@/components/earn/Backtest";

const EXCHANGE = DEPLOYMENTS.perpl.exchange;
const daysAgo = (n: number) => new Date(Date.now() - n * 864e5).toISOString().slice(0, 10);
const signed = (s: string | undefined) => (s === undefined || s === "" ? "text-muted-foreground" : Number(s) >= 0 ? "text-positive" : "text-destructive");
const away = (px: number, mark: number) => `${px >= mark ? "+" : ""}${(((px - mark) / mark) * 100).toFixed(1)}% from mark`;

const usePerplMarkets = () => useAsync("perpl-markets", getPerplMarkets);
const useFunding30 = (perpId: number | undefined) => useAsync(perpId ? `funding30:${perpId}` : null, () => getFunding(perpId!, daysAgo(30)));

// "Live from Perpl" source line: which numbers come from the API vs the exchange contract
function Source({ className }: { className?: string }) {
  return (
    <a
      href={addressUrl(EXCHANGE)}
      target="_blank"
      rel="noreferrer"
      className={cn("inline-flex items-center gap-1.5 text-[11px] text-muted-foreground hover:text-foreground", className)}
    >
      <ProtocolIcon protocol="perpl" size={14} />
      Perpl API + exchange contract
      <ExternalLink className="size-3" />
    </a>
  );
}

function Head({ title, children }: { title: React.ReactNode; children?: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="flex min-w-0 items-center gap-2 font-medium">
        <ProtocolIcon protocol="perpl" size={20} />
        <span className="truncate">{title}</span>
      </span>
      {children}
    </div>
  );
}

function Live() {
  return (
    <span className="inline-flex shrink-0 items-center gap-1.5 rounded-full bg-positive/10 px-2 py-0.5 text-[11px] text-positive">
      <span className="size-1.5 animate-pulse rounded-full bg-current" /> Live
    </span>
  );
}

function Kv({ k, v, note, tone }: { k: string; v: React.ReactNode; note?: React.ReactNode; tone?: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{k}</dt>
      <dd className={cn("tnum mt-0.5 truncate text-base font-medium", tone)}>{v}</dd>
      {note && <dd className="tnum mt-0.5 truncate text-[11px] text-muted-foreground">{note}</dd>}
    </div>
  );
}

// ── 1. Baskets page: funding across the hedge markets ────────────────────────────

export function FundingStrip() {
  const m = usePerplMarkets();
  return (
    <Panel className="space-y-5 p-5 sm:p-6">
      <Head title="Perpl funding">
        <Live />
      </Head>
      {m.error && <ErrorBox message={`Perpl unavailable: ${m.error}`} />}
      <div className="grid gap-3 md:grid-cols-3">
        {m.data
          ? m.data.markets.map((x) => <FundingTile key={x.perpId} m={x} />)
          : !m.error && [0, 1, 2].map((i) => <Skeleton key={i} className="h-36 rounded-2xl" />)}
      </div>
      <Source />
    </Panel>
  );
}

function FundingTile({ m }: { m: PerplMarket }) {
  const f = useFunding30(m.perpId);
  const series = (f.data ?? []).map((d) => Number(d.apr));
  const oi = Number(m.shortOi) * Number(m.mark);
  return (
    <div className="space-y-3 rounded-2xl border border-border bg-secondary/30 p-4">
      <div className="flex items-center gap-2">
        <TokenIcon symbol={m.asset} size={22} />
        <span className="font-medium">{m.name}-PERP</span>
        <span className="tnum ml-auto text-sm text-muted-foreground">{fmtUsd(Number(m.mark))}</span>
      </div>
      <div>
        <p className="text-xs text-muted-foreground">Short funding, 7d APR</p>
        <p className={cn("tnum text-2xl font-medium", signed(m.fundingApr7d))}>{pct(m.fundingApr7d)}</p>
      </div>
      {f.data ? <Sparkline data={series} area className="h-10 w-full" /> : <Skeleton className="h-10 w-full" />}
      <p className="tnum flex justify-between text-[11px] text-muted-foreground">
        <span>30d daily funding</span>
        <span>
          OI {fmtUsdCompact(oi)} · max {Math.round(1 / Number(m.initMargin))}×
        </span>
      </p>
    </div>
  );
}

// ── 2. Builder: what the bot will watch for this basis leg ────────────────────

export function BasisRiskCard({ asset, leverage, gate }: { asset: string; leverage: number; gate: { apr: number; days: number } | null }) {
  const m = usePerplMarkets();
  const mk = m.data?.markets.find((x) => x.asset === asset);
  const f = useFunding30(mk?.perpId);
  if (m.error) return <ErrorBox message={`Perpl unavailable: ${m.error}`} />;
  if (!m.data || !mk) return <Skeleton className="h-72 w-full rounded-3xl" />;

  const mark = Number(mk.mark);
  const maint = Number(mk.maintMargin);
  const buffer = Number(m.data.guardBuffer);
  const L = leverage > 0 ? leverage : 1;
  // short with collateral = notional / L: liquidation where (equity + (P − P')q) / P'q = maintenance
  const liq = (mark * (1 + 1 / L)) / (1 + maint);
  const guard = (mark * (1 + 1 / L)) / (1 + maint + buffer);
  // gate input exactly as the scheduler reads it: avg of the last `days` complete daily APRs, else Perpl's live 7d
  const tail = gate && gate.days > 0 && f.data ? f.data.slice(-gate.days) : [];
  const gateApr = tail.length ? tail.reduce((a, d) => a + Number(d.apr), 0) / tail.length : mk.fundingApr7d ? Number(mk.fundingApr7d) : null;
  const on = gate && gateApr !== null ? gateApr >= gate.apr : null;

  return (
    <Panel className="space-y-5 p-5">
      <Head title={`Perpl ${mk.name} short · ${L}×`}>
        <Live />
      </Head>

      <dl className="grid grid-cols-3 gap-3">
        <Kv k="Funding 7d" v={pct(mk.fundingApr7d)} tone={signed(mk.fundingApr7d)} />
        <Kv k="Liquidation" v={fmtUsd(liq)} tone="text-destructive" note={away(liq, mark)} />
        <Kv k="Guard" v={fmtUsd(guard)} tone="text-warning" note={away(guard, mark)} />
      </dl>

      <div className="space-y-2">
        {f.data ? <FundingBars data={f.data} threshold={gate?.apr ?? null} /> : <Skeleton className="h-28 w-full" />}
        <p className="flex items-center justify-between gap-3 text-[11px] text-muted-foreground">
          <span>30d daily funding</span>
          {gate && on !== null && (
            <span className={cn("tnum rounded-full px-2 py-0.5", on ? "bg-positive/10 text-positive" : "bg-warning/10 text-warning")}>
              {on ? "Short on" : "Parked"} · {pct(gateApr, 1)} vs gate {pct(gate.apr, 1)}
            </span>
          )}
        </p>
      </div>

      <details className="group text-xs text-muted-foreground">
        <summary className="flex cursor-pointer list-none items-center gap-1 hover:text-foreground [&::-webkit-details-marker]:hidden">
          How the guard works
          <ChevronDown className="size-3.5 transition-transform group-open:rotate-180" />
        </summary>
        <ol className="mt-2 list-decimal space-y-1 pl-4 leading-5">
          <li>Fires at {pct(maint + buffer, 0)} margin ({pct(maint, 0)} maintenance + {pct(buffer, 0)})</li>
          <li>Adds free balance, then USDC from your wallet</li>
          <li>Trims the short if that is not enough</li>
          <li>Closes to lending as a last resort</li>
        </ol>
      </details>
      <Source />
    </Panel>
  );
}

function FundingBars({ data, threshold }: { data: FundingDay[]; threshold: number | null }) {
  const rows = data.map((d) => ({ day: d.day.slice(5), apr: Number(d.apr) }));
  return (
    <div className="h-28 w-full">
      <ResponsiveContainer width="100%" height="100%" initialDimension={{ width: 1, height: 1 }}>
        <BarChart data={rows} margin={{ top: 4, right: 0, bottom: 0, left: 0 }}>
          <XAxis dataKey="day" tick={{ fill: "var(--muted-foreground)", fontSize: 10 }} axisLine={false} tickLine={false} minTickGap={40} />
          <YAxis hide domain={["auto", "auto"]} />
          <Tooltip
            cursor={{ fill: "var(--secondary)" }}
            contentStyle={{ background: "var(--popover)", border: "1px solid var(--border)", borderRadius: 12, fontSize: 12 }}
            formatter={(v) => [pct(Number(v)), "APR"]}
          />
          <ReferenceLine y={0} stroke="var(--border)" />
          {threshold !== null && <ReferenceLine y={threshold} stroke="var(--warning)" strokeDasharray="4 4" />}
          <Bar dataKey="apr" radius={[2, 2, 0, 0]} isAnimationActive={false}>
            {rows.map((r, i) => (
              <Cell key={i} fill={r.apr >= 0 ? "var(--positive)" : "var(--destructive)"} />
            ))}
          </Bar>
        </BarChart>
      </ResponsiveContainer>
    </div>
  );
}

// ── 3. Portfolio: the live short, as the scheduler sees it ────────────────────

const NEXT: Record<PerplRisk["next"], (r: PerplRisk) => string> = {
  none: () => "Healthy. No action on the next pass.",
  guard_top_up: (r) => `Next pass: guard tops up ${fmtUsd(Number(r.guard.topUp))} of margin.`,
  guard_cut: (r) => `Next pass: guard trims the short by ${r.guard.lotCut} ${r.name}.`,
  guard_close: () => "Next pass: guard closes the short and parks it in lending.",
  gate_close: () => "Next pass: funding below the gate, closes the short and parks it in lending.",
  gate_open: () => "Next pass: funding recovered above gate + band, reopens the short.",
};

export function PositionRisk({ positionId }: { positionId: string }) {
  const r = useAsync(`perpl:${positionId}`, () => getPositionPerpl(positionId));
  if (r.error) return <ErrorBox message={`Perpl risk unavailable: ${r.error}`} />;
  if (!r.data) return <Skeleton className="h-40 w-full rounded-2xl" />;
  return (
    <>
      {r.data.map((x) => (
        <RiskBlock key={x.perpId} r={x} />
      ))}
    </>
  );
}

function RiskBlock({ r }: { r: PerplRisk }) {
  const alert = r.next !== "none";
  return (
    <div className="space-y-4 rounded-2xl border border-border bg-secondary/30 p-4">
      <Head title={`Perpl ${r.name} short`}>
        {r.account > 0 && <span className="tnum text-[11px] text-muted-foreground">account #{r.account}</span>}
      </Head>
      {!r.size ? (
        <p className="text-xs text-muted-foreground">{r.account > 0 ? "No open short. Basis is parked in lending." : "No Perpl account yet."}</p>
      ) : (
        <>
          <MarginGauge frac={Number(r.marginFrac)} maint={Number(r.maintMargin)} guardAt={Number(r.guardAt)} />
          <dl className="grid grid-cols-2 gap-4 sm:grid-cols-3">
            <Kv k="Short" v={`${r.size} ${r.name}`} note={`entry ${fmtUsd(Number(r.entry))}`} />
            <Kv k="Mark" v={fmtUsd(Number(r.mark))} note={r.markValid ? "valid" : "stale, guard paused"} />
            <Kv k="Collateral" v={fmtUsd(Number(r.collateral))} note={`equity ${fmtUsd(Number(r.equity))}`} />
            <Kv k="Liquidation" v={fmtUsd(Number(r.liqPrice))} tone="text-destructive" note={away(Number(r.liqPrice), Number(r.mark))} />
            <Kv k="Guard fires at" v={fmtUsd(Number(r.guardPrice))} tone="text-warning" note={away(Number(r.guardPrice), Number(r.mark))} />
            <Kv k="Funding earned" v={`${Number(r.funding) >= 0 ? "+" : ""}${fmtUsd(Number(r.funding))}`} tone={signed(r.funding)} note={`short PnL ${fmtUsd(Number(r.pnl))}`} />
          </dl>
        </>
      )}
      {r.gate && (
        <p className="tnum text-xs text-muted-foreground">
          Funding gate: {r.gate.lookbackDays || 7}d APR <span className={signed(r.gate.apr)}>{pct(r.gate.apr)}</span> vs {pct(r.gate.threshold, 1)}{" "}
          threshold → {r.gate.want ? "keep short" : "park in lending"}
        </p>
      )}
      <p className={cn("rounded-xl px-3 py-2 text-xs", alert ? "bg-warning/10 text-warning" : "bg-positive/10 text-positive")}>{NEXT[r.next](r)}</p>
    </div>
  );
}

// margin fraction on a 0..max scale with maintenance (liquidation) and guard markers
function MarginGauge({ frac, maint, guardAt }: { frac: number; maint: number; guardAt: number }) {
  const max = Math.max(frac * 1.25, guardAt * 2.5);
  const at = (x: number) => `${Math.min(100, (x / max) * 100)}%`;
  const tone = frac < maint ? "bg-destructive" : frac < guardAt ? "bg-warning" : "bg-positive";
  return (
    <div className="space-y-1.5">
      <p className="flex justify-between text-xs">
        <span className="text-muted-foreground">Margin</span>
        <span className="tnum font-medium">{pct(frac, 1)}</span>
      </p>
      <div className="relative h-2 rounded-full bg-secondary">
        <div className={cn("h-full rounded-full", tone)} style={{ width: at(frac) }} />
        <span className="absolute -top-1 h-4 w-0.5 bg-destructive" style={{ left: at(maint) }} />
        <span className="absolute -top-1 h-4 w-0.5 bg-warning" style={{ left: at(guardAt) }} />
      </div>
      <p className="tnum flex gap-3 text-[11px] text-muted-foreground">
        <span className="text-destructive">| liquidation {pct(maint, 0)}</span>
        <span className="text-warning">| guard {pct(guardAt, 0)}</span>
      </p>
    </div>
  );
}
