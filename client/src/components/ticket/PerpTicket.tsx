"use client";

import { useEffect, useRef, useState } from "react";
import { TxState } from "@/components/table/TxState";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Dropdown } from "@/components/ui/Dropdown";
import { ErrorState } from "@/components/ui/ErrorState";
import { Segmented } from "@/components/ui/Segmented";
import {
  CHAIN_ID,
  errorMessage,
  fromWad,
  toUnits,
  txTier,
  type Market,
  type OrderResponse,
} from "@/lib/api";
import { formatSize, formatUsd } from "@/lib/format";
import { useCreateOrder, useExecutions, usePerplAccount } from "@/lib/query";
import { useSession } from "@/lib/session";
import { isZero } from "@/lib/units";
import { AmountInput } from "./AmountInput";
import { SideToggle, type Side } from "./SideToggle";
import { SummaryRows } from "./SummaryRows";

const STOP_REASON =
  "Stop orders are not wired yet: Perpl has trigger orders, but this terminal places market and limit only.";

const TYPES = [
  { value: "market", label: "Market" },
  { value: "limit", label: "Limit" },
  { value: "stop", label: "Stop", disabled: true, title: STOP_REASON },
] as const;

const SLIPPAGE = [
  { value: "10", label: "0.1% slippage" },
  { value: "50", label: "0.5% slippage" },
  { value: "100", label: "1% slippage" },
] as const;

// what the receipt says happened; a landed IOC can match nothing
const OUTCOME: Record<string, (r: OrderResponse) => string> = {
  filled: () => "Filled in full.",
  partial: () => "Partly filled; the rest was cancelled, because a market order never rests.",
  not_filled: () =>
    "Nothing matched inside your slippage bound, so no position changed. Only gas was spent.",
  resting: (r) => `Resting on the book as order ${r.info?.order_id ?? "?"}.`,
  submitted: () =>
    "The transaction was sent but its receipt has not arrived, so the outcome is not known yet.",
};

export function PerpTicket({ market }: { market?: Market | null }) {
  const [side, setSide] = useState<Side>("long");
  const [type, setType] = useState<"market" | "limit" | "stop">("market");
  const maxLev = Math.max(1, Math.floor((market?.maxLeverage ?? 100) / 100));
  const [leverage, setLeverage] = useState(Math.min(10, maxLev));
  const [size, setSize] = useState("");
  const [price, setPrice] = useState("");
  const [postOnly, setPostOnly] = useState(false);
  const [slippage, setSlippage] = useState<"10" | "50" | "100">("50");
  const [result, setResult] = useState<OrderResponse | null>(null);

  const { authenticated, login } = useSession();
  const account = usePerplAccount();
  const executions = useExecutions();
  const create = useCreateOrder();
  const key = useRef(crypto.randomUUID());

  useEffect(() => {
    key.current = crypto.randomUUID();
  }, [market?.id, side, type, size, price, leverage]);
  // a market's cap differs: 15x on BTC, 3x on MON
  useEffect(() => setLeverage((l) => Math.min(Math.max(l, 1), maxLev)), [maxLev]);

  const limit = type === "limit";
  const lot = market && size ? toUnits(size, market.sizeDecimals) : "";
  const rawPrice = market && limit && price ? toUnits(price, market.priceDecimals) : "";
  const refPrice = limit ? Number(price) : market?.mark ? fromWad(market.mark) : NaN;
  const notional = Number(size) * refPrice;
  const margin = notional / leverage;

  const unfunded = account.data?.registered === false;
  const blocked = !market
    ? "Pick a market before placing an order."
    : unfunded
      ? `You have no Perpl account yet. Deposit at least ${fromWad(account.data!.min_open, account.data!.collateral.decimals)} ${account.data!.collateral.symbol} above to open one.`
      : lot && isZero(lot)
        ? `This market sizes in ${market.sizeDecimals} decimals, so that size rounds to zero.`
        : limit && rawPrice && isZero(rawPrice)
          ? `This market prices in ${market.priceDecimals} decimals, so that price rounds to zero.`
          : null;
  const filled = !!lot && (!limit || !!rawPrice);

  const submit = async () => {
    if (!market || !filled || blocked) return;
    setResult(null);
    const common = {
      market_id: market.id,
      venue: market.venue,
      side: side === "long" ? ("buy" as const) : ("sell" as const),
      chain_id: CHAIN_ID,
      idempotency_key: key.current,
    };
    try {
      const res = await create.mutateAsync(
        limit
          ? { ...common, order_type: "perp_limit", price: rawPrice, quantity: lot, leverage: leverage * 100, post_only: postOnly }
          : { ...common, order_type: "perp_market", quantity: lot, leverage: leverage * 100, max_slippage_bps: Number(slippage) },
      );
      setResult(res);
      setSize("");
    } catch {
      // the message renders under the button
    }
  };

  const exec = result
    ? executions.data?.executions.find((e) => e.intent_key === result.intent_key)
    : undefined;

  return (
    <Card className="space-y-4 p-4">
      <div className="flex items-center justify-between gap-3">
        <span className="rounded-full bg-secondary/70 px-4 py-2 text-sm">Isolated</span>
        <span className="tnum rounded-full bg-secondary/70 px-4 py-2 text-sm font-medium">
          {leverage}x
        </span>
      </div>

      <SideToggle value={side} onChange={setSide} />
      <Segmented
        id="perp-order-type"
        value={type}
        onChange={setType}
        options={TYPES}
        tone="accent"
        className="grid-cols-3"
      />

      <div className="space-y-2">
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <span>Leverage</span>
          <span className="tnum">
            {leverage}x of {maxLev}x max
          </span>
        </div>
        <input
          type="range"
          min={1}
          max={maxLev}
          value={leverage}
          onChange={(e) => setLeverage(Number(e.target.value))}
          aria-label="Leverage"
          className="range"
          style={{ "--pct": `${maxLev > 1 ? ((leverage - 1) / (maxLev - 1)) * 100 : 100}%` } as React.CSSProperties}
        />
      </div>

      {limit && (
        <AmountInput label="Limit price" value={price} onChange={setPrice} suffix={market?.quote ?? ""} />
      )}
      <AmountInput label="Size" value={size} onChange={setSize} suffix={market?.base ?? ""} />
      {limit ? (
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          <input
            type="checkbox"
            checked={postOnly}
            onChange={(e) => setPostOnly(e.target.checked)}
            className="size-4 accent-[var(--primary)]"
          />
          Post only — reject rather than take, so this order can only make
        </label>
      ) : (
        <div className="flex justify-end">
          <Dropdown value={slippage} onChange={setSlippage} options={SLIPPAGE} ariaLabel="Max slippage" />
        </div>
      )}

      <div className="h-px bg-border" />

      <SummaryRows
        rows={[
          { label: "Notional", value: Number.isFinite(notional) && notional > 0 ? formatUsd(notional) : null },
          {
            label: `Margin (${market?.quote ?? ""})`,
            value: Number.isFinite(margin) && margin > 0 ? formatSize(margin, 2) : null,
          },
          {
            label: "Free collateral",
            value:
              account.data?.registered && account.data.balance
                ? formatSize(
                    fromWad(
                      (BigInt(account.data.balance) - BigInt(account.data.locked ?? "0")).toString(),
                      account.data.collateral.decimals,
                    ),
                    2,
                  )
                : null,
          },
          { label: limit ? "Type" : "Max slippage", value: limit ? `GTC${postOnly ? " · post only" : ""}` : `${Number(slippage) / 100}%` },
        ]}
      />

      {authenticated ? (
        <Button className="w-full" onClick={submit} pending={create.isPending} disabled={!filled || !!blocked}>
          {side === "long" ? "Long" : "Short"} {market?.base ?? ""}
        </Button>
      ) : (
        <Button className="w-full" onClick={login}>
          Sign in to trade
        </Button>
      )}

      {authenticated && blocked && <ErrorState message={blocked} />}
      {create.error && <ErrorState message={errorMessage(create.error)} />}

      {result &&
        (result.error ? (
          <ErrorState message={result.error} />
        ) : (
          <div className="space-y-2 rounded-xl border border-border bg-secondary/30 p-3">
            <div className="flex items-center gap-2">
              <TxState tier={txTier(exec) ?? "seen"} />
              <span className="text-xs text-muted-foreground">{result.state}</span>
            </div>
            <p className="text-xs">{(OUTCOME[result.state] ?? ((r: OrderResponse) => r.info?.reason ?? r.state))(result)}</p>
            {result.tx_hash && (
              <p className="tnum text-[11px] break-all text-muted-foreground">{result.tx_hash}</p>
            )}
          </div>
        ))}
    </Card>
  );
}
