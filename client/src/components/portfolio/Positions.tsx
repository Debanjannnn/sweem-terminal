"use client";

import Link from "next/link";
import { Fragment, useState } from "react";
import { formatUnits } from "viem";
import { ChevronDown } from "lucide-react";
import { errText, fmtTime, fmtUsd, withdrawPosition } from "@/lib/api";
import { FEE_BPS } from "@/lib/config";
import { useTickers } from "@/lib/hooks";
import { fmtUnits, tokenBySymbol } from "@/lib/tokens";
import type { Position, Strategy } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Button, Notice, StatusPill } from "@/components/ui";
import { TokenIcon } from "@/components/TokenIcon";
import { IconStack } from "@/components/IconStack";
import { ScheduleForm } from "@/components/automation/ScheduleForm";
import { ExecutionProgress } from "@/components/Execution";
import { KIND_LABEL, parts, venueName, protocolOf } from "@/components/earn/sources";
import { ProtocolIcon } from "@/components/ProtocolIcon";
import { PositionRisk } from "@/components/perpl/PerplRisk";
import { Segmented } from "@/components/motion";

const SELL_PCTS = ["25", "50", "75", "100"] as const;
export const STABLES = new Set(["USDC", "AUSD", "USDT0"]);
export const PRICE_OF: Record<string, string> = { WMON: "MON" };

type PriceOf = (sym: string) => number | null;

// spot USD price per token symbol from live tickers (stables = $1); null = no quote yet
export function usePriceOf(): PriceOf {
  const { tickers } = useTickers();
  return (sym) => (STABLES.has(sym) ? 1 : (tickers[PRICE_OF[sym] ?? sym]?.price ?? null));
}

// legs grouped by sleeve token (old rows: no token = position asset)
const groupsOf = (p: Position) =>
  Object.entries(Object.groupBy(p.legs, (l) => l.token ?? p.asset)).map(([token, legs = []]) => ({ token, legs, dec: tokenBySymbol(token)?.decimals ?? 18 }));

// Entry / mark on the basket's own index (Σ weight × token price, as the basket chart). A token's entry is its effective
// fill: USDC put into it (principal after the open fee × weight, less Perpl collateral) ÷ tokens received.
// ponytail: legs change on rebalance and LP legs hold only the MON half, so entry drifts for those; store fill prices at deploy if exact entry matters
export function positionPrices(p: Position, s: Strategy | undefined, priceOf: PriceOf) {
  if (!s) return { entry: null, mark: null };
  const net = Number(p.principalUsdc) * (1 - FEE_BPS / 10000);
  const groups = groupsOf(p);
  let entry: number | null = 0;
  let mark: number | null = 0;
  for (const sl of parts(s)) {
    const w = sl.weightBps / 10000;
    const px = priceOf(sl.asset);
    mark = mark === null || px === null ? null : mark + w * px;
    if (STABLES.has(sl.asset)) {
      entry = entry === null ? null : entry + w;
      continue;
    }
    const g = groups.find((x) => x.token === sl.asset);
    const held = g?.legs.reduce((a, l) => a + Number(formatUnits(BigInt(l.asset || 0), g.dec)), 0) ?? 0;
    const coll = g?.legs.reduce((a, l) => a + (l.collateral ? Number(formatUnits(BigInt(l.collateral), 6)) : 0), 0) ?? 0;
    entry = entry === null || held <= 0 ? null : entry + (w * (net * w - coll)) / held;
  }
  return { entry, mark };
}

// spot value: supplied tokens at spot + Perpl collateral at $1
// ponytail: hedge PnL not marked (~0 for a delta-neutral short); read venue balances via RPC if exact value matters
function valueOf(p: Position, priceOf: PriceOf) {
  return groupsOf(p).reduce<number | null>((v, g) => {
    const px = priceOf(g.token);
    if (v === null || px === null) return null;
    return g.legs.reduce((s, l) => s + Number(formatUnits(BigInt(l.asset || 0), g.dec)) * px + (l.collateral ? Number(formatUnits(BigInt(l.collateral), 6)) : 0), v);
  }, 0);
}

const signed = (n: number) => (n >= 0 ? "text-positive" : "text-destructive");

export function PositionsTable({
  positions,
  strategyOf,
  onChange,
}: {
  positions: Position[];
  strategyOf: (id: string) => Strategy | undefined;
  onChange: () => void;
}) {
  const priceOf = usePriceOf();
  const [open, setOpen] = useState<string | null>(null);
  return (
    <div className="overflow-x-auto rounded-3xl border border-border bg-card">
      <table className="w-full min-w-[860px] text-sm">
        <thead className="text-left text-xs text-muted-foreground">
          <tr className="border-b border-border">
            <th className="py-3 pl-5 font-normal">Basket</th>
            <th className="px-3 py-3 text-right font-normal">Size</th>
            <th className="px-3 py-3 text-right font-normal">Entry price</th>
            <th className="px-3 py-3 text-right font-normal">Mark price</th>
            <th className="px-3 py-3 text-right font-normal">Value</th>
            <th className="px-3 py-3 text-right font-normal">PnL (ROE)</th>
            <th className="px-3 py-3 font-normal">Opened</th>
            <th className="px-3 py-3 font-normal">Status</th>
            <th className="py-3 pr-5" />
          </tr>
        </thead>
        <tbody className="tnum">
          {positions.map((p) => {
            const s = strategyOf(p.strategyId);
            const { entry, mark } = positionPrices(p, s, priceOf);
            const value = valueOf(p, priceOf);
            const principal = Number(p.principalUsdc);
            const pnl = value !== null ? value - principal : null;
            const expanded = open === p.id;
            return (
              <Fragment key={p.id}>
                <tr
                  onClick={() => setOpen(expanded ? null : p.id)}
                  className={cn("cursor-pointer border-b border-border transition-colors hover:bg-secondary/30", expanded && "bg-secondary/20")}
                >
                  <td className="py-3 pl-5">
                    <div className="flex items-center gap-3">
                      <IconStack symbols={groupsOf(p).map((g) => g.token)} size={22} ring="ring-card" />
                      <Link href={`/baskets/${p.strategyId}`} onClick={(e) => e.stopPropagation()} className="truncate font-medium hover:underline">
                        {p.strategyName}
                      </Link>
                    </div>
                  </td>
                  <td className="px-3 py-3 text-right">{fmtUsd(principal)}</td>
                  <td className="px-3 py-3 text-right">{entry !== null ? fmtUsd(entry) : "-"}</td>
                  <td className="px-3 py-3 text-right">{mark !== null ? fmtUsd(mark) : "-"}</td>
                  <td className="px-3 py-3 text-right">{value !== null ? fmtUsd(value) : "-"}</td>
                  <td className={cn("px-3 py-3 text-right", pnl !== null && signed(pnl))}>
                    {pnl !== null ? `${pnl >= 0 ? "+" : ""}${fmtUsd(pnl)} (${pnl >= 0 ? "+" : ""}${((pnl / principal) * 100).toFixed(2)}%)` : "-"}
                  </td>
                  <td className="px-3 py-3 text-xs whitespace-nowrap text-muted-foreground">{fmtTime(p.openedAt)}</td>
                  <td className="px-3 py-3">
                    <StatusPill status={p.status} />
                  </td>
                  <td className="py-3 pr-5 text-right">
                    <ChevronDown className={cn("inline size-4 text-muted-foreground transition-transform", expanded && "rotate-180")} />
                  </td>
                </tr>
                {expanded && (
                  <tr className="border-b border-border bg-secondary/10">
                    <td colSpan={9} className="px-5 py-4">
                      <PositionDetail p={p} value={value} onChange={onChange} />
                    </td>
                  </tr>
                )}
              </Fragment>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

// expanded row: legs, Perpl risk, sell, TP/SL/rebalance, live execution
function PositionDetail({ p, value, onChange }: { p: Position; value: number | null; onChange: () => void }) {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [execId, setExecId] = useState<string | null>(null);
  const [auto, setAuto] = useState(false);
  const [sellPct, setSellPct] = useState<(typeof SELL_PCTS)[number]>("100");
  // withdrawing = a full close that stalled; only a 100% retry resumes it
  const closing = p.status === "withdrawing";
  const groups = groupsOf(p);

  async function withdraw() {
    setBusy(true);
    setErr(null);
    try {
      setExecId((await withdrawPosition(p.id, closing ? 10000 : Number(sellPct) * 100)).executionId);
    } catch (e) {
      setErr(errText(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-3">
      <ul className="grid gap-x-8 gap-y-1 text-xs md:grid-cols-2">
        {groups.flatMap((g) =>
          g.legs.map((l, i) => (
            <li key={`${g.token}${i}`} className="flex items-center gap-2">
              <TokenIcon symbol={g.token} size={14} />
              <span className="rounded-md bg-secondary/60 px-1.5 py-0.5 text-[10px] text-muted-foreground">{KIND_LABEL[l.kind as keyof typeof KIND_LABEL] ?? l.kind}</span>
              <ProtocolIcon protocol={protocolOf(l.venue)} size={14} />
              {venueName(l.venue)}
              <span className="tnum ml-auto text-muted-foreground">
                {fmtUnits(l.asset || "0", g.dec)} {g.token}
                {l.entryPx ? ` · short entry ${fmtUsd(Number(l.entryPx))}` : ""}
              </span>
            </li>
          )),
        )}
      </ul>
      <p className="text-[11px] text-muted-foreground">
        {p.lastRebalanceAt ? `Rebalanced ${fmtTime(p.lastRebalanceAt)}. ` : ""}Value marks supplied tokens at spot; lending/staking yield accrues on-venue.
      </p>
      {p.status !== "closed" && p.legs.some((l) => l.kind === "basis") && <PositionRisk positionId={p.id} />}
      {p.legs.some((l) => l.kind === "lp") && <Notice>ActiveVault leg: 4-day lock, withdraw may fail inside the lock.</Notice>}
      {err && <Notice tone="bad">{err}</Notice>}
      {(p.status === "open" || closing) && (
        <div className="flex flex-wrap items-center gap-2">
          {!closing && (
            <Segmented id={`sell-${p.id}`} value={sellPct} onChange={setSellPct} options={SELL_PCTS.map((v) => ({ value: v, label: `${v}%` }))} className="w-auto" />
          )}
          <Button variant="ghost" pending={busy} onClick={() => void withdraw()}>
            {closing ? "Resume sell" : "Sell"} to USDC
          </Button>
          {!closing && (
            <Button variant="ghost" onClick={() => setAuto((a) => !a)}>
              {auto ? "Hide automation" : "TP / SL / Rebalance"}
            </Button>
          )}
        </div>
      )}
      {auto && <ScheduleForm target={{ type: "position", id: p.id }} current={value} onCreated={() => setAuto(false)} />}
      {execId && <ExecutionProgress id={execId} onSettled={onChange} />}
    </div>
  );
}
