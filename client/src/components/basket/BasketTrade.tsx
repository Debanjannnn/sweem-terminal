"use client";

import { useState } from "react";
import { basketBuy, basketSell, errText, fmtUsd, getBasketQuote } from "@/lib/api";
import { useAsync, useSession } from "@/lib/session";
import { fmtUnits, MARKETS, parseAmount, USDC } from "@/lib/tokens";
import type { Basket, Holdings } from "@/lib/types";
import { Button, Notice, Spinner } from "@/components/ui";
import { ClientRunProgress, useClientTrade } from "@/components/ClientTrade";
import { ExecutionProgress } from "@/components/Execution";
import { actionBtn, balanceOf, pill, Row, SidePicker, Slippage, useDebounced } from "@/components/trade/MarketPanel";
import { formatUnits } from "viem";

// Basket buy (one market buy per leg) / sell (bps of the attributed holding): run by the delegated signer when enabled, else signed by the user leg by leg.
export function BasketTrade({
  basket,
  holdings,
  heldUsd,
  onDone,
}: {
  basket: Basket;
  holdings: Holdings | null;
  heldUsd: number | null;
  onDone: () => void;
}) {
  const { ready, authenticated, login, address, delegated } = useSession();
  const [side, setSide] = useState<"buy" | "sell">("buy");
  const [amount, setAmount] = useState("");
  const [pct, setPct] = useState(100);
  const [slippage, setSlippage] = useState(100);
  const { run, start, busy: signing } = useClientTrade(`${side === "buy" ? "Buy" : "Sell"} ${basket.name}`);
  const [sending, setSending] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [execId, setExecId] = useState<string | null>(null);
  const busy = signing || sending;

  const raw = parseAmount(amount, USDC.decimals);
  const debounced = useDebounced(side === "buy" && raw && raw > BigInt(0) ? raw : null);
  const usdc = balanceOf(holdings, "USDC");
  const held = holdings?.baskets.find((b) => b.basketId === basket.id);
  const holds = !!held?.legs.some((l) => Number(l.amount) > 0);
  // sell legs = attributed holding × pct, zero legs dropped (nothing to quote)
  const sellPct = useDebounced(pct);
  const sellLegs = (held?.legs ?? []).map((l) => ({ market: l.market, amount: (BigInt(l.raw) * BigInt(sellPct)) / BigInt(100) })).filter((l) => l.amount > BigInt(0));
  const quote = useAsync(
    side === "buy"
      ? debounced !== null ? `bq:${basket.id}:${debounced}:${slippage}` : null
      : sellLegs.length ? `bqs:${basket.id}:${sellLegs.map((l) => l.amount).join(",")}:${slippage}` : null,
    () => getBasketQuote(side === "buy" ? { side, amountUsdc: debounced!, legs: basket.legs } : { side, legs: sellLegs }, slippage),
  );
  const q = side === "buy" ? (raw !== null && raw === debounced ? quote.data : null) : sellPct === pct ? quote.data : null;

  const reason = !ready
    ? "Loading…"
    : !address
      ? "Wallet loading…"
      : side === "sell"
        ? holds
          ? null
          : "Nothing held in this basket"
        : amount.trim() === ""
          ? "Enter an amount"
          : raw === null || raw === BigInt(0)
            ? "Invalid amount"
            : usdc !== null && raw > usdc
              ? "Insufficient USDC"
              : quote.error
                ? errText(new Error(quote.error))
                : !q || quote.loading
                  ? "Fetching quote…"
                  : !q.ok
                    ? "A leg can't be quoted"
                    : null;

  async function submit() {
    if (delegated) {
      setSending(true);
      setErr(null);
      setExecId(null);
      try {
        const r =
          side === "buy"
            ? await basketBuy(basket.id, { amountUsdc: amount.trim(), slippageBps: slippage })
            : await basketSell(basket.id, { bps: pct * 100, slippageBps: slippage });
        setExecId(r.executionId);
      } catch (e) {
        setErr(errText(e));
      } finally {
        setSending(false);
      }
      return;
    }
    const ex = await start(
      side === "buy"
        ? { kind: "basket_buy", basketId: basket.id, amountUsdc: amount.trim(), slippageBps: slippage }
        : { kind: "basket_sell", basketId: basket.id, bps: pct * 100, slippageBps: slippage },
    );
    if (ex?.status === "confirmed") setAmount("");
    onDone();
  }

  return (
    <div className="space-y-4 p-5">
      <div className="flex items-center justify-between gap-3">
        <SidePicker id="bk-side" value={side} onChange={setSide} options={[["buy", "Buy"], ["sell", "Sell"]]} />
        <Slippage value={slippage} onChange={setSlippage} />
      </div>

      {side === "buy" ? (
        <div className="rounded-2xl bg-card p-4">
          <div className="flex items-center justify-between text-sm text-muted-foreground">
            Spend
            {usdc !== null && (
              <button type="button" onClick={() => setAmount(formatUnits(usdc, 6))} className="tnum text-xs hover:text-foreground">
                Max {fmtUnits(usdc, 6, 2)}
              </button>
            )}
          </div>
          <div className="mt-2 flex items-center gap-3">
            <input
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              inputMode="decimal"
              aria-label="Amount in USDC"
              placeholder="0"
              className="tnum w-full min-w-0 bg-transparent text-3xl font-medium tracking-tight outline-none placeholder:text-muted-foreground/50"
            />
            {pill("USDC")}
          </div>
        </div>
      ) : (
        <div className="space-y-2 rounded-2xl bg-card p-4">
          <div className="flex items-center justify-between text-sm text-muted-foreground">
            Sell <span className="tnum text-foreground">{pct}% of holding</span>
          </div>
          <input
            type="range"
            min={1}
            max={100}
            value={pct}
            onChange={(e) => setPct(Number(e.target.value))}
            className="range w-full"
            style={{ "--pct": `${pct}%` } as React.CSSProperties}
          />
          <div className="tnum text-sm text-muted-foreground">≈ {heldUsd !== null ? fmtUsd((heldUsd * pct) / 100) : "-"} before slippage</div>
        </div>
      )}

      {quote.loading && !q && (side === "sell" || debounced !== null) && <Spinner label="Quoting legs…" />}
      {q && (
        <dl className="space-y-1.5 px-1 text-xs">
          {q.legs.map((l) => {
            const m = MARKETS.find((x) => x.orderbook === l.market.toLowerCase());
            const sym = m?.symbol ?? l.market;
            return (
              <Row key={l.market} label={side === "buy" ? `${sym} · ${(l.weightBps ?? 0) / 100}%` : `${m ? fmtUnits(l.amount ?? "0", m.token.decimals) : "-"} ${sym}`}>
                {l.quote && m ? (
                  <>
                    {side === "buy" ? `${fmtUnits(l.quote.expectedOut, m.token.decimals)} ${sym}` : `${fmtUnits(l.quote.expectedOut, USDC.decimals, 2)} USDC`}
                    <span className={l.quote.impactBps > 100 ? " text-warning" : " text-muted-foreground"}> · {(l.quote.impactBps / 100).toFixed(2)}%</span>
                  </>
                ) : (
                  <span className="text-destructive">{l.error ? errText(new Error(l.error)) : "-"}</span>
                )}
              </Row>
            );
          })}
          {side === "sell" && q.expectedOutUsdc && (
            <Row label="You receive">
              ≈ {fmtUnits(q.expectedOutUsdc, USDC.decimals, 2)} USDC
              <span className="text-muted-foreground"> · min {fmtUnits(q.minOutUsdc ?? "0", USDC.decimals, 2)}</span>
            </Row>
          )}
        </dl>
      )}
      {q?.partial && <Notice>Some legs only partially fill at this size; the unfilled {side === "buy" ? "USDC" : "tokens"} stay in your wallet.</Notice>}
      {q?.legs.some((l) => l.quote && BigInt(l.quote.amount) > BigInt(l.quote.maxSafeAmount)) && (
        <Notice>At least one leg is above its safe size for current book depth; consider a smaller amount.</Notice>
      )}
      {!authenticated && ready ? (
        <Button className={actionBtn} onClick={login}>
          Log in to trade
        </Button>
      ) : (
        <Button className={actionBtn} pending={busy} disabled={busy || reason !== null} onClick={() => void submit()}>
          {busy ? (delegated ? "Submitting…" : "Sign in your wallet…") : (reason ?? `${side === "buy" ? "Buy" : "Sell"} ${basket.name}`)}
        </Button>
      )}
      {err && <Notice tone="bad">{err}</Notice>}
      {run && <ClientRunProgress run={run} />}
      {execId && (
        <ExecutionProgress
          id={execId}
          onSettled={(e) => {
            if (e.status === "confirmed") setAmount("");
            onDone();
          }}
        />
      )}
      <p className="text-[11px] leading-4 text-muted-foreground">
        Monad mainnet, real funds. One Kuru market order per leg (plus approvals),{" "}
        {delegated ? "run by your delegated signer from your Privy wallet." : "each signed by you in order from your Privy wallet."}
        Tokens stay in your wallet; the basket is just a label on them.
      </p>
    </div>
  );
}
