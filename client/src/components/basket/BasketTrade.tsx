"use client";

import { useState } from "react";
import { errText, fmtUsd, getBasketQuote } from "@/lib/api";
import { useAsync, useSession } from "@/lib/session";
import { fmtUnits, marketOf, parseAmount, USDC } from "@/lib/tokens";
import type { Basket, Holdings } from "@/lib/types";
import { Button, Notice, Spinner } from "@/components/ui";
import { ClientRunProgress, useClientTrade } from "@/components/ClientTrade";
import { actionBtn, balanceOf, pill, Row, SidePicker, Slippage, useDebounced } from "@/components/trade/MarketPanel";
import { formatUnits } from "viem";

// Basket buy (one market buy per leg) / sell (bps of the attributed holding), signed by the user leg by leg.
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
  const { ready, authenticated, login, address } = useSession();
  const [side, setSide] = useState<"buy" | "sell">("buy");
  const [amount, setAmount] = useState("");
  const [pct, setPct] = useState(100);
  const [slippage, setSlippage] = useState(100);
  const { run, start, busy } = useClientTrade(`${side === "buy" ? "Buy" : "Sell"} ${basket.name}`);

  const raw = parseAmount(amount, USDC.decimals);
  const debounced = useDebounced(side === "buy" && raw && raw > BigInt(0) ? raw : null);
  const usdc = balanceOf(holdings, "USDC");
  const quote = useAsync(debounced !== null ? `bq:${basket.id}:${debounced}:${slippage}` : null, () =>
    getBasketQuote(basket.legs, debounced!, slippage),
  );
  const q = raw !== null && raw === debounced ? quote.data : null;
  const held = holdings?.baskets.find((b) => b.basketId === basket.id);
  const holds = !!held?.legs.some((l) => Number(l.amount) > 0);

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
          <div className="tnum text-sm text-muted-foreground">≈ {heldUsd !== null ? fmtUsd((heldUsd * pct) / 100) : "—"} before slippage</div>
        </div>
      )}

      {side === "buy" && quote.loading && debounced !== null && <Spinner label="Quoting legs…" />}
      {q && (
        <dl className="space-y-1.5 px-1 text-xs">
          {q.legs.map((l, i) => {
            const leg = basket.legs[i];
            const m = marketOf(leg?.market ?? "");
            return (
              <Row key={l.market} label={`${m?.symbol ?? l.market} · ${(leg?.weightBps ?? 0) / 100}%`}>
                {l.quote && m ? (
                  <>
                    {fmtUnits(l.quote.expectedOut, m.token.decimals)} {m.symbol}
                    <span className={l.quote.impactBps > 100 ? " text-warning" : " text-muted-foreground"}> · {(l.quote.impactBps / 100).toFixed(2)}%</span>
                  </>
                ) : (
                  <span className="text-destructive">{l.error ? errText(new Error(l.error)) : "—"}</span>
                )}
              </Row>
            );
          })}
        </dl>
      )}
      {q?.partial && <Notice>Some legs only partially fill at this size; the unfilled USDC stays in your wallet.</Notice>}
      {q?.legs.some((l) => l.quote && BigInt(l.quote.amount) > BigInt(l.quote.maxSafeAmount)) && (
        <Notice>At least one leg is above its safe size for current book depth; consider a smaller amount.</Notice>
      )}
      {!authenticated && ready ? (
        <Button className={actionBtn} onClick={login}>
          Log in to trade
        </Button>
      ) : (
        <Button className={actionBtn} pending={busy} disabled={busy || reason !== null} onClick={() => void submit()}>
          {busy ? "Sign in your wallet…" : (reason ?? `${side === "buy" ? "Buy" : "Sell"} ${basket.name}`)}
        </Button>
      )}
      {run && <ClientRunProgress run={run} />}
      <p className="text-[11px] leading-4 text-muted-foreground">
        Monad mainnet, real funds. One Kuru market order per leg (plus approvals), each signed by you in order from your Privy wallet.
        Tokens stay in your wallet; the basket is just a label on them.
      </p>
    </div>
  );
}
