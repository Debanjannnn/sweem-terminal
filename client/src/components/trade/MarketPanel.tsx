"use client";

import { useState } from "react";
import { useDebounced } from "@/lib/hooks";
import { ArrowDown } from "lucide-react";
import { errText, fmtBps, fmtUsd, getQuote } from "@/lib/api";
import { useAsync, useSession } from "@/lib/session";
import { fmtUnits, parseAmount, type MarketDef } from "@/lib/tokens";
import type { Holdings } from "@/lib/types";
import { Button, Notice, Row, Spinner } from "@/components/ui";
import { TokenIcon } from "@/components/TokenIcon";
import { ClientRunProgress, useClientTrade } from "@/components/ClientTrade";
import { Flash, Segmented } from "@/components/motion";
import { formatUnits } from "viem";
import { AnyChainButton } from "@/components/deposit/AnyChainDeposit";

const SLIPPAGES = [50, 100, 200] as const;
export const SIDES = [
  { value: "buy", label: "Buy" },
  { value: "sell", label: "Sell" },
] as const;

export const balanceOf = (h: Holdings | null, symbol: string) => {
  const t = h?.tokens.find((x) => x.symbol === symbol);
  return t ? BigInt(t.raw) : h ? BigInt(0) : null;
};

// "Slippage" label + bps chips
export function Slippage({ value, onChange }: { value: number; onChange: (v: number) => void }) {
  return (
    <div className="flex items-center gap-1 text-xs text-muted-foreground">
      Slippage
      <Segmented
        id="slippage"
        size="xs"
        value={String(value)}
        onChange={(v) => onChange(Number(v))}
        options={SLIPPAGES.map((s) => ({ value: String(s), label: `${s / 100}%` }))}
      />
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

// Market buy/sell signed by the user (prepare → Privy wallet → record); quote from market-data for preview + caps.
export function MarketPanel({ market, holdings, onDone }: { market: MarketDef; holdings: Holdings | null; onDone: () => void }) {
  const { ready, authenticated, login, address } = useSession();
  const [side, setSide] = useState<"buy" | "sell">("buy");
  const [amount, setAmount] = useState("");
  const [slippage, setSlippage] = useState(100);

  const sym = market.token.symbol;
  const { run, start, busy } = useClientTrade(`${side === "buy" ? "Buy" : "Sell"} ${sym}`);
  const inTok = side === "buy" ? market.quote : market.token;
  const outTok = side === "buy" ? market.token : market.quote;
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
    : !address
      ? "Wallet loading…"
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
    const ex = await start({ kind: "trade", market: market.symbol, side, amount: amount.trim(), slippageBps: slippage });
    if (ex?.status === "confirmed") setAmount("");
    onDone();
  }

  return (
    <div className="space-y-4 p-5">
      <div className="flex items-center justify-between gap-3">
        <Segmented id="mkt-side" size="sm" value={side} onChange={(s) => (setSide(s), setAmount(""))} options={SIDES} />
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
            <span className={q.impactBps > 100 ? "text-warning" : undefined}>{fmtBps(q.impactBps)}</span>
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
          Above {fmtBps(q.maxImpactBps)} impact.{" "}
          <button className="underline underline-offset-2" onClick={() => setAmount(formatUnits(maxSafe, inTok.decimals))}>
            Use max safe size
          </button>
        </Notice>
      )}

      {!authenticated && ready ? (
        <Button className={actionBtn} onClick={login}>
          Log in to trade
        </Button>
      ) : (
        <Button className={actionBtn} pending={busy} disabled={busy || reason !== null} onClick={() => void submit()}>
          {busy ? "Sign in your wallet…" : (reason ?? `${side === "buy" ? "Buy" : "Sell"} ${sym}`)}
        </Button>
      )}
      {inTok.symbol === "USDC" && raw !== null && balance !== null && raw > balance && (
        <AnyChainButton need={Number(raw - balance) / 1e6} onArrived={onDone} className="mx-auto flex w-fit" />
      )}
      {run && <ClientRunProgress run={run} />}
      <p className="text-[11px] leading-4 text-muted-foreground">
        Monad mainnet, real funds. Market order on the Kuru {sym}/{market.quote.symbol} book, signed by you from your Privy wallet (an approve first when
        needed). Reverts if you&apos;d receive less than the minimum at your slippage. Gas is paid in MON.
      </p>
    </div>
  );
}
