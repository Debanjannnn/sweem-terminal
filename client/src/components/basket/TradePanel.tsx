"use client";

import { useEffect, useState } from "react";
import { formatUnits, type Address } from "viem";
import { getQuote } from "@/lib/api";
import { SHARE_DECIMALS, USDC_DECIMALS } from "@/lib/config";
import { useAsync, useSession } from "@/lib/session";
import { baseToken, fmtUnits, parseAmount } from "@/lib/tokens";
import { useTx } from "@/lib/tx";
import type { Market } from "@/lib/types";
import { ArrowDown } from "lucide-react";
import { Button, Spinner } from "@/components/ui";
import { TokenIcon } from "@/components/TokenIcon";
import { cn } from "@/lib/utils";

export type Balances = { usdc: bigint; shares: bigint; mon: bigint };
type Side = "mint" | "redeem";

const SLIPPAGES = [50, 100, 200] as const;

const quoteError = (side: Side, e: string) =>
  e === "unavailable"
    ? `No liquidity to ${side === "mint" ? "buy" : "sell"} right now`
    : e === "stale"
      ? "Price feed is stale, try again shortly"
      : e;

export function TradePanel({
  basket,
  symbol,
  markets,
  balances,
  onTraded,
}: {
  basket: Address;
  symbol: string;
  markets: Market[] | null;
  balances: Balances | null;
  onTraded: () => void;
}) {
  const { ready, authenticated, address, login } = useSession();
  const { mint, redeem } = useTx();
  const [side, setSide] = useState<Side>("mint");
  const [amount, setAmount] = useState("");
  const [slippage, setSlippage] = useState<number>(100);
  const [busy, setBusy] = useState(false);

  const decimals = side === "mint" ? USDC_DECIMALS : SHARE_DECIMALS;
  const outDecimals = side === "mint" ? SHARE_DECIMALS : USDC_DECIMALS;
  const inSym = side === "mint" ? "USDC" : symbol;
  const outSym = side === "mint" ? symbol : "USDC";
  const raw = parseAmount(amount, decimals);
  const balance = balances ? (side === "mint" ? balances.usdc : balances.shares) : null;

  const [debounced, setDebounced] = useState<bigint | null>(null);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(raw && raw > BigInt(0) ? raw : null), 400);
    return () => clearTimeout(t);
  }, [raw]);

  const quote = useAsync(
    debounced !== null ? `quote:${basket}:${side}:${debounced}:${slippage}` : null,
    () => getQuote(basket, side, debounced!, slippage),
  );
  const q = raw !== null && raw === debounced ? quote.data : null;

  const reason = !ready
    ? "Loading…"
    : !authenticated
      ? null
      : !address
        ? "Setting up wallet…"
        : amount.trim() === ""
          ? "Enter an amount"
          : raw === null || raw === BigInt(0)
            ? "Invalid amount"
            : balance !== null && raw > balance
              ? `Insufficient ${inSym}`
              : balances?.mon === BigInt(0)
                ? "Need MON for gas"
                : quote.error
                  ? quoteError(side, quote.error)
                  : !q || quote.loading
                    ? "Fetching quote…"
                    : q.stale
                      ? "Quote is stale"
                      : BigInt(q.minOut) === BigInt(0)
                        ? "Amount too small"
                        : null;

  async function submit() {
    if (!q || raw === null) return;
    setBusy(true);
    try {
      if (side === "mint") await mint(basket, raw, BigInt(q.minOut));
      else await redeem(basket, raw, BigInt(q.minOut));
      setAmount("");
      onTraded();
    } catch {
      // useTx already toasted
    } finally {
      setBusy(false);
    }
  }

  const pill = (sym: string) => (
    <span className="inline-flex shrink-0 items-center gap-1.5 rounded-full bg-secondary py-1.5 pl-1.5 pr-3 text-sm font-medium">
      <TokenIcon symbol={sym === "USDC" ? "USDC" : sym} size={20} />
      {sym}
    </span>
  );

  return (
    <div className="space-y-4 p-5">
      <div className="flex items-center justify-between gap-3">
        <div role="tablist" className="flex rounded-lg bg-foreground/[0.05] p-0.5">
          {(["mint", "redeem"] as const).map((s) => (
            <button
              key={s}
              role="tab"
              aria-selected={side === s}
              onClick={() => (setSide(s), setAmount(""))}
              className={cn(
                "rounded-md px-3 py-1 text-sm font-medium transition-colors",
                side === s ? "bg-card text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground",
              )}
            >
              {s === "mint" ? "Buy" : "Sell"}
            </button>
          ))}
        </div>
        <div className="flex items-center gap-1 text-xs text-muted-foreground">
          Slippage
          {SLIPPAGES.map((s) => (
            <button
              key={s}
              type="button"
              onClick={() => setSlippage(s)}
              className={cn(
                "rounded-md px-2 py-1 font-medium transition-colors",
                s === slippage ? "bg-card text-foreground shadow-sm" : "hover:text-foreground",
              )}
            >
              {s / 100}%
            </button>
          ))}
        </div>
      </div>

      <div className="relative rounded-2xl bg-card">
        <div className="p-4">
          <div className="flex items-center justify-between text-sm text-muted-foreground">
            Spend
            {balance !== null && (
              <button
                type="button"
                onClick={() => setAmount(formatUnits(balance, decimals))}
                className="tnum text-xs transition-colors hover:text-foreground"
              >
                Max {fmtUnits(balance, decimals)}
              </button>
            )}
          </div>
          <div className="mt-2 flex items-center gap-3">
            <input
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              inputMode="decimal"
              aria-label={`Amount in ${inSym}`}
              placeholder="0"
              className="tnum w-full min-w-0 bg-transparent text-3xl font-medium tracking-tight outline-none placeholder:text-muted-foreground/50"
            />
            {pill(inSym)}
          </div>
        </div>
        <div className="relative h-px bg-border">
          <span className="absolute left-1/2 top-1/2 grid size-8 -translate-x-1/2 -translate-y-1/2 place-items-center rounded-full border border-border bg-card">
            <ArrowDown className="size-4" />
          </span>
        </div>
        <div className="p-4">
          <div className="text-sm text-muted-foreground">Receive</div>
          <div className="mt-2 flex items-center gap-3">
            <div className="tnum w-full min-w-0 truncate text-3xl font-medium tracking-tight">
              {quote.loading && debounced !== null ? (
                <Spinner />
              ) : q ? (
                fmtUnits(q.expectedOut, outDecimals)
              ) : (
                <span className="text-muted-foreground/50">0</span>
              )}
            </div>
            {pill(outSym)}
          </div>
        </div>
      </div>

      {q && (
        <dl className="space-y-1.5 px-1 text-xs">
          <Row label="Minimum received">{`${fmtUnits(q.minOut, outDecimals)} ${outSym}`}</Row>
          {q.legs.map((l) => (
            <Row key={l.market} label={`${baseToken(l.market, markets)?.symbol ?? "leg"} price impact`}>
              <span className={l.impactBps > 100 ? "text-warning" : undefined}>{(l.impactBps / 100).toFixed(2)}%</span>
            </Row>
          ))}
        </dl>
      )}

      {!authenticated && ready ? (
        <Button className="w-full rounded-xl py-3.5 text-base hover:rounded-xl" onClick={login}>
          Log in to trade
        </Button>
      ) : (
        <Button
          className="w-full rounded-xl py-3.5 text-base hover:rounded-xl"
          pending={busy}
          disabled={reason !== null}
          onClick={() => void submit()}
        >
          {busy ? (side === "mint" ? "Buying…" : "Selling…") : (reason ?? `${side === "mint" ? "Buy" : "Sell"} ${symbol}`)}
        </Button>
      )}
      {balances?.mon === BigInt(0) && (
        <p className="text-xs text-warning">
          Your wallet has no MON for gas.{" "}
          <a href="https://faucet.monad.xyz" target="_blank" rel="noreferrer" className="underline underline-offset-2">
            Get testnet MON
          </a>
        </p>
      )}
      <p className="text-[11px] leading-4 text-muted-foreground">
        Monad testnet only; tokens have no real value. Each order fills every component on its Kuru order book in one
        transaction. Prices move between quote and fill; the trade reverts if you receive less than the minimum at your
        slippage setting.
      </p>
    </div>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-4">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="tnum truncate">{children}</dd>
    </div>
  );
}
