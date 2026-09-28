"use client";

import { useEffect, useState } from "react";
import { formatUnits, type Address } from "viem";
import { getQuote } from "@/lib/api";
import { SHARE_DECIMALS, USDC_DECIMALS } from "@/lib/config";
import { useAsync, useSession } from "@/lib/session";
import { baseToken, fmtUnits, parseAmount } from "@/lib/tokens";
import { useTx } from "@/lib/tx";
import type { Market } from "@/lib/types";
import { Button, Divider, Label, Panel, Spinner } from "@/components/ui";
import { Segmented } from "@/components/motion";
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

  return (
    <Panel>
      <div className="p-5">
        <Segmented
          id="trade-side"
          value={side}
          onChange={(s) => {
            setSide(s);
            setAmount("");
          }}
          options={[
            { value: "mint", label: "Buy" },
            { value: "redeem", label: "Sell" },
          ]}
        />
      </div>
      <Divider />
      <div className="p-5">
        <div className="flex items-center justify-between">
          <Label>{side === "mint" ? "You pay" : "You sell"}</Label>
          {balance !== null && (
            <button
              type="button"
              onClick={() => setAmount(formatUnits(balance, decimals))}
              className="tnum text-sm text-muted-foreground transition-colors hover:text-foreground"
            >
              Max {fmtUnits(balance, decimals)} {inSym}
            </button>
          )}
        </div>
        <div className="mt-4 flex items-center gap-3">
          <input
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            inputMode="decimal"
            aria-label={`Amount in ${inSym}`}
            placeholder="0"
            className="tnum w-full min-w-0 bg-transparent text-4xl font-medium tracking-tight outline-none placeholder:text-muted-foreground/40"
          />
          <span className="inline-flex shrink-0 items-center gap-2 rounded-full bg-secondary/70 py-2 pl-3 pr-4 text-sm font-medium">
            {side === "mint" && <TokenIcon symbol="USDC" size={20} />}
            {inSym}
          </span>
        </div>
      </div>
      <Divider />
      <dl className="space-y-2 px-5 py-4 text-sm">
        <Row label="Expected">
          {quote.loading && debounced !== null ? (
            <Spinner />
          ) : q ? (
            `${fmtUnits(q.expectedOut, outDecimals)} ${outSym}`
          ) : (
            "—"
          )}
        </Row>
        <Row label="Minimum out">{q ? `${fmtUnits(q.minOut, outDecimals)} ${outSym}` : "—"}</Row>
        <Row label="Max slippage">
          <span className="inline-flex gap-1">
            {SLIPPAGES.map((s) => (
              <button
                key={s}
                type="button"
                onClick={() => setSlippage(s)}
                className={cn(
                  "rounded-md px-2 py-0.5 text-xs transition-colors",
                  s === slippage ? "bg-secondary text-foreground" : "text-muted-foreground hover:text-foreground",
                )}
              >
                {s / 100}%
              </button>
            ))}
          </span>
        </Row>
        {q?.legs.map((l) => (
          <Row key={l.market} label={`${baseToken(l.market, markets)?.symbol ?? "leg"} impact`}>
            <span className={l.impactBps > 100 ? "text-warning" : undefined}>{(l.impactBps / 100).toFixed(2)}%</span>
          </Row>
        ))}
      </dl>
      <Divider />
      <div className="space-y-3 p-5">
        {!authenticated && ready ? (
          <Button className="w-full py-3" onClick={login}>
            Log in to trade
          </Button>
        ) : (
          <Button className="w-full py-3" pending={busy} disabled={reason !== null} onClick={() => void submit()}>
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
      </div>
    </Panel>
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
