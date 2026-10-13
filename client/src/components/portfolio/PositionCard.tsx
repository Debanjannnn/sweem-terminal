"use client";

import Link from "next/link";
import { useState } from "react";
import { formatUnits } from "viem";
import { errText, fmtTime, fmtUsd, withdrawPosition } from "@/lib/api";
import { useTickers } from "@/lib/hooks";
import { fmtUnits, tokenBySymbol } from "@/lib/tokens";
import type { Position } from "@/lib/types";
import { Button, Notice, Panel, StatusPill } from "@/components/ui";
import { TokenIcon } from "@/components/TokenIcon";
import { IconStack } from "@/components/IconStack";
import { ScheduleForm } from "@/components/automation/ScheduleForm";
import { ExecutionProgress } from "@/components/Execution";
import { KIND_LABEL, venueName, protocolOf } from "@/components/earn/sources";
import { ProtocolIcon } from "@/components/ProtocolIcon";
import { PositionRisk } from "@/components/perpl/PerplRisk";
import { Segmented } from "@/components/motion";

const SELL_PCTS = ["25", "50", "75", "100"] as const;
export const STABLES = new Set(["USDC", "AUSD", "USDT0"]);
export const PRICE_OF: Record<string, string> = { WMON: "MON" };

// spot USD price per token symbol from live tickers (stables = $1); null = no quote yet
export function usePriceOf() {
  const { tickers } = useTickers();
  return (sym: string) => (STABLES.has(sym) ? 1 : (tickers[PRICE_OF[sym] ?? sym]?.price ?? null));
}

// principal is what went in; per-source amounts are the atoms supplied at deploy (venue yield accrues on-venue)
export function PositionCard({ p, priceOf, onChange }: { p: Position; priceOf: (sym: string) => number | null; onChange: () => void }) {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [execId, setExecId] = useState<string | null>(null);
  const [auto, setAuto] = useState(false);
  const [sellPct, setSellPct] = useState<(typeof SELL_PCTS)[number]>("100");
  // withdrawing = a full close that stalled; only a 100% retry resumes it
  const closing = p.status === "withdrawing";
  // legs grouped by sleeve token (old rows: no token = position asset); each group marked at its own price
  const groups = Object.entries(Object.groupBy(p.legs, (l) => l.token ?? p.asset)).map(([token, legs = []]) => ({ token, legs, dec: tokenBySymbol(token)?.decimals ?? 18 }));
  // ponytail: basis AUSD collateral counted at $1, hedge PnL not marked (~0); read venue balances via RPC if exact value matters
  const value = groups.reduce<number | null>((v, g) => {
    const px = priceOf(g.token);
    if (v === null || px === null) return null;
    return g.legs.reduce((s, l) => s + Number(formatUnits(BigInt(l.asset || 0), g.dec)) * px + (l.collateral ? Number(formatUnits(BigInt(l.collateral), 6)) : 0), v);
  }, 0);
  const pnl = value !== null ? value - Number(p.principalUsdc) : null;

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
    <Panel className="space-y-3 p-5">
      <div className="flex items-center gap-3">
        <IconStack symbols={groups.map((g) => g.token)} size={28} ring="ring-card" />
        <Link href={`/baskets/${p.strategyId}`} className="truncate font-medium hover:underline">
          {p.strategyName}
        </Link>
        <span className="ml-auto">
          <StatusPill status={p.status} />
        </span>
      </div>
      <dl className="tnum grid grid-cols-3 gap-3 text-sm">
        <div>
          <dt className="text-xs text-muted-foreground">Principal</dt>
          <dd>{fmtUsd(Number(p.principalUsdc))}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">Value (spot)</dt>
          <dd>{value !== null ? fmtUsd(value) : "-"}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">PnL</dt>
          <dd className={pnl === null ? "" : pnl >= 0 ? "text-positive" : "text-destructive"}>{pnl !== null ? `${pnl >= 0 ? "+" : ""}${fmtUsd(pnl)}` : "-"}</dd>
        </div>
      </dl>
      <ul className="space-y-1 text-xs">
        {groups.flatMap((g) =>
          g.legs.map((l, i) => (
            <li key={`${g.token}${i}`} className="flex items-center gap-2">
              {groups.length > 1 && <TokenIcon symbol={g.token} size={14} />}
              <span className="rounded-md bg-secondary/60 px-1.5 py-0.5 text-[10px] text-muted-foreground">{KIND_LABEL[l.kind as keyof typeof KIND_LABEL] ?? l.kind}</span>
              <ProtocolIcon protocol={protocolOf(l.venue)} size={14} />
              {venueName(l.venue)}
              <span className="tnum ml-auto text-muted-foreground">
                {fmtUnits(l.asset || "0", g.dec)} {g.token}
                {l.entryPx ? ` · entry ${fmtUsd(Number(l.entryPx))}` : ""}
              </span>
            </li>
          )),
        )}
      </ul>
      <p className="text-[11px] text-muted-foreground">
        Opened {fmtTime(p.openedAt)}
        {p.lastRebalanceAt ? ` · rebalanced ${fmtTime(p.lastRebalanceAt)}` : ""}. Value marks supplied tokens at spot; lending/staking yield accrues on-venue.
      </p>
      {p.status !== "closed" && p.legs.some((l) => l.kind === "basis") && <PositionRisk positionId={p.id} />}
      {p.legs.some((l) => l.kind === "lp") && <Notice>ActiveVault leg: 4-day lock, withdraw may fail inside the lock.</Notice>}
      {err && <Notice tone="bad">{err}</Notice>}
      {(p.status === "open" || closing) && (
        <div className="flex flex-wrap items-center gap-2">
          {!closing && (
            <Segmented
              id={`sell-${p.id}`}
              value={sellPct}
              onChange={setSellPct}
              options={SELL_PCTS.map((v) => ({ value: v, label: `${v}%` }))}
              className="w-auto flex-1"
            />
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
    </Panel>
  );
}
