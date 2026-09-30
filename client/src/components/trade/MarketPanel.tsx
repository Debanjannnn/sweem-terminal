"use client";

import { useEffect, useState } from "react";
import { ArrowDown } from "lucide-react";
import { errText, fmtUsd, getQuote, trade } from "@/lib/api";
import { useAsync, useSession } from "@/lib/session";
import { fmtUnits, parseAmount, USDC, type MarketDef } from "@/lib/tokens";
import type { Holdings } from "@/lib/types";
import { Button, Notice, Spinner } from "@/components/ui";
import { TokenIcon } from "@/components/TokenIcon";
import { ExecutionProgress } from "@/components/Execution";
import { DelegationCard } from "@/components/automation/Delegation";
import { cn } from "@/lib/utils";
import { Flash, TabPill } from "@/components/motion";
import { formatUnits } from "viem";

export const SLIPPAGES = [50, 100, 200] as const;

export function useDebounced<T>(v: T, ms = 400) {
  const [d, setD] = useState(v);
  useEffect(() => {
    const t = setTimeout(() => setD(v), ms);
    return () => clearTimeout(t);
  }, [v, ms]);
  return d;
}

export const balanceOf = (h: Holdings | null, symbol: string) => {
  const t = h?.tokens.find((x) => x.symbol === symbol);
  return t ? BigInt(t.raw) : h ? BigInt(0) : null;
};

export function SidePicker<T extends string>({ id, value, onChange, options }: { id: string; value: T; onChange: (v: T) => void; options: [T, string][] }) {
  return (
    <div role="tablist" className="flex rounded-lg bg-foreground/[0.05] p-0.5">
      {options.map(([v, label]) => (
        <button
          key={v}
          role="tab"
          aria-selected={value === v}
          onClick={() => onChange(v)}
          className={cn("relative rounded-md px-3 py-1 text-sm font-medium transition-colors", value === v ? "text-foreground" : "text-muted-foreground hover:text-foreground")}
        >
          {value === v && <TabPill id={id} className="rounded-md bg-card shadow-sm" />}
          <span className="relative">{label}</span>
        </button>
      ))}
    </div>
  );
}

export function Slippage({ value, onChange }: { value: number; onChange: (v: number) => void }) {
  return (
    <div className="flex items-center gap-1 text-xs text-muted-foreground">
      Slippage
      {SLIPPAGES.map((s) => (
        <button
          key={s}
          type="button"
          onClick={() => onChange(s)}
          className={cn("relative rounded-md px-2 py-1 font-medium transition-colors", s === value ? "text-foreground" : "hover:text-foreground")}
        >
          {s === value && <TabPill id="slippage" className="rounded-md bg-card shadow-sm" />}
          <span className="relative">{s / 100}%</span>
        </button>
      ))}
    </div>
  );
}

export function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-4">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="tnum truncate">{children}</dd>
    </div>
  );
}

export const pill = (sym: string) => (
  <span className="inline-flex shrink-0 items-center gap-1.5 rounded-full bg-secondary py-1.5 pl-1.5 pr-3 text-sm font-medium">
    <TokenIcon symbol={sym} size={20} />
    {sym}
  </span>
);

export const actionBtn = "w-full rounded-xl py-3.5 text-base hover:rounded-xl";

// Market buy/sell via POST /me/trade (executor, delegated signer); quote from market-data for preview + caps.
export function MarketPanel({ market, holdings, onDone }: { market: MarketDef; holdings: Holdings | null; onDone: () => void }) {
  const { ready, authenticated, login, delegated } = useSession();
  const [side, setSide] = useState<"buy" | "sell">("buy");
  const [amount, setAmount] = useState("");
  const [slippage, setSlippage] = useState(100);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [execId, setExecId] = useState<string | null>(null);

  const sym = market.symbol;
  const inTok = side === "buy" ? USDC : market.token;
  const outTok = side === "buy" ? market.token : USDC;
  const raw = parseAmount(amount, inTok.decimals);
  const debounced = useDebounced(raw && raw > BigInt(0) ? raw : null);
  const balance = balanceOf(holdings, inTok.symbol);

  const quote = useAsync(debounced !== null ? `q:${market.orderbook}:${side}:${debounced}:${slippage}` : null, () =>
    getQuote(market.orderbook, side, debounced!, slippage),
  );
  const q = raw !== null && raw === debounced ? quote.data : null;
  const maxSafe = q ? BigInt(q.maxSafeAmount) : null;
  const overSafe = q && raw !== null && maxSafe !== null && raw > maxSafe;

  const reason = !ready
    ? "Loading…"
    : !delegated
      ? "Enable delegation to trade"
      : amount.trim() === ""
        ? "Enter an amount"
        : raw === null || raw === BigInt(0)
          ? "Invalid amount"
          : balance !== null && raw > balance
            ? `Insufficient ${inTok.symbol}`
            : quote.error
              ? errText(new Error(quote.error))
              : !q || quote.loading
                ? "Fetching quote…"
                : overSafe
                  ? "Above safe size for book depth"
                  : null;

  async function submit() {
    setBusy(true);
    setErr(null);
    try {
      const r = await trade({ market: sym, side, amount: amount.trim(), slippageBps: slippage });
      setExecId(r.executionId);
      setAmount("");
    } catch (e) {
      setErr(errText(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-4 p-5">
      <div className="flex items-center justify-between gap-3">
        <SidePicker id="mkt-side" value={side} onChange={(s) => (setSide(s), setAmount(""))} options={[["buy", "Buy"], ["sell", "Sell"]]} />
        <Slippage value={slippage} onChange={setSlippage} />
      </div>

      <div className="relative rounded-2xl bg-card">
        <div className="p-4">
          <div className="flex items-center justify-between text-sm text-muted-foreground">
            {side === "buy" ? "Spend" : "Sell"}
            {balance !== null && (
              <button type="button" onClick={() => setAmount(formatUnits(balance, inTok.decimals))} className="tnum text-xs transition-colors hover:text-foreground">
                Max {fmtUnits(balance, inTok.decimals)}
              </button>
            )}
          </div>
          <div className="mt-2 flex items-center gap-3">
            <input
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              inputMode="decimal"
              aria-label={`Amount in ${inTok.symbol}`}
              placeholder="0"
              className="tnum w-full min-w-0 bg-transparent text-3xl font-medium tracking-tight outline-none placeholder:text-muted-foreground/50"
            />
            {pill(inTok.symbol)}
          </div>
        </div>
        <div className="relative h-px bg-border">
          <span className="absolute left-1/2 top-1/2 grid size-8 -translate-x-1/2 -translate-y-1/2 place-items-center rounded-full border border-border bg-card">
            <ArrowDown className="size-4" />
          </span>
        </div>
        <div className="p-4">
          <div className="text-sm text-muted-foreground">Receive (est.)</div>
          <div className="mt-2 flex items-center gap-3">
            <div className="tnum w-full min-w-0 truncate text-3xl font-medium tracking-tight">
              {quote.loading && debounced !== null ? (
                <Spinner />
              ) : q ? (
                <Flash value={Number(q.expectedOut)}>{fmtUnits(q.expectedOut, outTok.decimals)}</Flash>
              ) : (
                <span className="text-muted-foreground/50">0</span>
              )}
            </div>
            {pill(outTok.symbol)}
          </div>
        </div>
      </div>

      {q && (
        <dl className="space-y-1.5 px-1 text-xs">
          <Row label="Minimum received">{`${fmtUnits(q.minOut, outTok.decimals)} ${outTok.symbol}`}</Row>
          <Row label="Avg price">{fmtUsd(Number(q.avgPx))}</Row>
          <Row label="Price impact">
            <span className={q.impactBps > 100 ? "text-warning" : undefined}>{(q.impactBps / 100).toFixed(2)}%</span>
          </Row>
          <Row label="Max safe size">{`${fmtUnits(q.maxSafeAmount, inTok.decimals, 2)} ${inTok.symbol}`}</Row>
        </dl>
      )}
      {q?.partial && (
        <Notice>
          Book only absorbs {fmtUnits(q.fillable, inTok.decimals)} of {fmtUnits(q.amount, inTok.decimals)} {inTok.symbol}; the rest won&apos;t fill.
        </Notice>
      )}
      {overSafe && maxSafe !== null && (
        <Notice>
          Above {(q.maxImpactBps / 100).toFixed(2)}% impact.{" "}
          <button className="underline underline-offset-2" onClick={() => setAmount(formatUnits(maxSafe, inTok.decimals))}>
            Use max safe size
          </button>
        </Notice>
      )}
      {authenticated && !delegated && <DelegationCard compact />}
      {err && <Notice tone="bad">{err}</Notice>}

      {!authenticated && ready ? (
        <Button className={actionBtn} onClick={login}>
          Log in to trade
        </Button>
      ) : (
        <Button className={actionBtn} pending={busy} disabled={reason !== null} onClick={() => void submit()}>
          {busy ? "Submitting…" : (reason ?? `${side === "buy" ? "Buy" : "Sell"} ${sym}`)}
        </Button>
      )}
      {execId && <ExecutionProgress id={execId} onSettled={onDone} />}
      <p className="text-[11px] leading-4 text-muted-foreground">
        Monad mainnet, real funds. Market order on the Kuru {sym}/USDC book, run from your own wallet by sweem&apos;s delegated signer.
        Reverts if you&apos;d receive less than the minimum at your slippage.
      </p>
    </div>
  );
}
