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
  txTier,
  type Market,
  type OrderResponse,
} from "@/lib/api";
import { formatSize } from "@/lib/format";
import { useAccount, useCreateOrder, useExecutions } from "@/lib/query";
import { useSession } from "@/lib/session";
import {
  isZero,
  limitPrice,
  limitProblem,
  limitQuantity,
  marketSize,
  quoteNotional,
} from "@/lib/units";
import { AmountInput } from "./AmountInput";
import { OrderTypeTabs, type OrderType } from "./OrderTypeTabs";
import { SideToggle, type Side } from "./SideToggle";
import { SummaryRows } from "./SummaryRows";

const MARGIN_MODES = [
  { value: "isolated", label: "Isolated" },
  { value: "cross", label: "Cross" },
] as const;

const TIFS = [
  { value: "gtc", label: "GTC" },
  { value: "ioc", label: "IOC" },
  { value: "fok", label: "FOK" },
] as const;

const SLIPPAGE = [
  { value: "10", label: "0.1% slippage" },
  { value: "50", label: "0.5% slippage" },
  { value: "100", label: "1% slippage" },
] as const;

// what actually happened, per the executor's read-back after the receipt
const OUTCOME: Record<string, (r: OrderResponse) => string> = {
  resting: (r) => `Resting on the book in slot ${r.info?.slot_idx ?? "?"} as order ${r.info?.order_id ?? "?"}.`,
  not_resting: (r) =>
    `It did not rest: ${r.info?.reason ?? "the slot read back empty after the transaction confirmed"}.`,
  filled_or_expired: () => "An IOC or FOK order never rests: it filled or expired on entry.",
  submitted: () =>
    "The transaction was sent but its receipt has not arrived, so the outcome is not known yet.",
};

export function OrderTicket({ market }: { market?: Market | null }) {
  const [margin, setMargin] = useState<"isolated" | "cross">("isolated");
  const [side, setSide] = useState<Side>("long");
  const [type, setType] = useState<OrderType>("market");
  const [leverage, setLeverage] = useState(10);
  const [price, setPrice] = useState("");
  const [quantity, setQuantity] = useState("");
  const [amount, setAmount] = useState("");
  const [tif, setTif] = useState<"gtc" | "ioc" | "fok">("gtc");
  const [postOnly, setPostOnly] = useState(false);
  const [slippage, setSlippage] = useState<"10" | "50" | "100">("50");
  const [result, setResult] = useState<OrderResponse | null>(null);

  const { authenticated, login } = useSession();
  const account = useAccount();
  const executions = useExecutions();
  const create = useCreateOrder();
  const key = useRef(crypto.randomUUID());

  const base = market?.base ?? "";
  const quoteSymbol = market?.quote ?? "USDC";
  const apiSide = side === "long" ? "buy" : "sell";
  const perp = market?.kind === "perp";
  const limit = type === "limit";
  const spend = apiSide === "buy" ? quoteSymbol : base;

  // a new attempt is a new order; a retry of the same one keeps its key
  useEffect(() => {
    key.current = crypto.randomUUID();
  }, [market?.id, side, type, amount, price, quantity]);

  const size = market && amount ? marketSize(market, apiSide, amount) : "";
  const rawPrice = market && price ? limitPrice(market, price) : "";
  const rawQty = market && quantity ? limitQuantity(market, quantity) : "";
  const filled = limit ? rawPrice !== "" && rawQty !== "" : size !== "";

  const unfunded = market?.venue === "kuru" && account.data?.registered === false;
  const blocked = !market
    ? "Pick a market before placing an order."
    : unfunded
      ? "This wallet has no funded Kuru trading account, so no order can be placed yet. Deposit above first."
      : !filled
        ? null
        : limit
          ? limitProblem(market, rawPrice, rawQty)
          : isZero(size)
            ? `That amount rounds to zero at ${spend}'s decimals, so name a larger one.`
            : null;

  const submit = async () => {
    if (!market || !filled || blocked) return;
    setResult(null);
    try {
      const res = await create.mutateAsync({
        market_id: market.id,
        venue: market.venue,
        side: apiSide,
        chain_id: CHAIN_ID,
        idempotency_key: key.current,
        ...(limit
          ? { order_type: "limit", price: rawPrice, quantity: rawQty, tif, post_only: postOnly }
          : { order_type: "market", size, max_slippage_bps: Number(slippage) }),
      });
      setResult(res);
      setAmount("");
      setQuantity("");
    } catch {
      // the message renders under the button; a toast would hide the 412 sentence
    }
  };

  const exec = result
    ? executions.data?.executions.find((e) => e.intent_key === result.intent_key)
    : undefined;
  // ponytail: no preview of a market order's average price, the quote endpoint
  // only takes a base size and a market buy is sized in quote atoms
  const notional =
    market && limit && rawPrice && rawQty
      ? formatSize(fromWad(quoteNotional(market, rawPrice, rawQty), market.quoteDecimals))
      : null;

  return (
    <Card className="space-y-4 p-4">
      {perp && (
        <div className="flex items-center justify-between gap-3">
          {/* ponytail: margin mode and leverage are display-only, wire them when a venue takes them */}
          <Dropdown
            value={margin}
            onChange={setMargin}
            options={MARGIN_MODES}
            ariaLabel="Margin mode"
          />
          <span className="tnum rounded-full bg-secondary/70 px-4 py-2 text-sm font-medium">
            {leverage}x
          </span>
        </div>
      )}

      <SideToggle value={side} onChange={setSide} labels={["Buy", "Sell"]} />
      <OrderTypeTabs value={type} onChange={setType} />

      {perp && (
        <div className="space-y-2">
          <div className="flex items-center justify-between text-xs text-muted-foreground">
            <span>Leverage</span>
            <span className="tnum">{leverage}x</span>
          </div>
          <input
            type="range"
            min={1}
            max={50}
            value={leverage}
            onChange={(e) => setLeverage(Number(e.target.value))}
            aria-label="Leverage"
            className="range"
            style={{ "--pct": `${((leverage - 1) / 49) * 100}%` } as React.CSSProperties}
          />
        </div>
      )}

      {limit ? (
        <>
          <AmountInput
            label="Limit price"
            value={price}
            onChange={setPrice}
            suffix={quoteSymbol}
          />
          <AmountInput
            label="Quantity"
            value={quantity}
            onChange={setQuantity}
            suffix={base || "base"}
          />
          <Segmented
            id="order-tif"
            value={tif}
            onChange={setTif}
            options={TIFS}
            className="grid-cols-3"
          />
          <label className="flex items-center gap-2 text-xs text-muted-foreground">
            <input
              type="checkbox"
              checked={postOnly}
              onChange={(e) => setPostOnly(e.target.checked)}
              className="size-4 accent-[var(--primary)]"
            />
            Post only — reject rather than take, so this order can only make
          </label>
        </>
      ) : (
        <>
          <AmountInput
            label={apiSide === "buy" ? "Amount to spend" : "Amount to sell"}
            value={amount}
            onChange={setAmount}
            suffix={spend || "size"}
          />
          <div className="flex justify-end">
            <Dropdown
              value={slippage}
              onChange={setSlippage}
              options={SLIPPAGE}
              ariaLabel="Max slippage"
            />
          </div>
        </>
      )}

      <div className="h-px bg-border" />

      <SummaryRows
        rows={[
          {
            label: limit ? `Notional (${quoteSymbol})` : `Spends (${spend})`,
            value: limit ? notional : amount || null,
          },
          {
            label: limit ? "Time in force" : "Max slippage",
            value: limit
              ? `${tif.toUpperCase()}${postOnly ? " · post only" : ""}`
              : `${Number(slippage) / 100}%`,
          },
        ]}
      />

      {authenticated ? (
        <Button
          className="w-full"
          onClick={submit}
          pending={create.isPending}
          disabled={!filled || !!blocked}
        >
          {limit ? "Place limit order" : "Place market order"}
        </Button>
      ) : (
        <Button className="w-full" onClick={login}>
          Sign in to trade
        </Button>
      )}

      {blocked && <ErrorState message={blocked} />}
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
            <p className="text-xs">
              {(OUTCOME[result.state] ?? ((r: OrderResponse) => r.info?.reason ?? r.state))(result)}
            </p>
            {result.tx_hash && (
              <p className="tnum text-[11px] break-all text-muted-foreground">
                {result.tx_hash}
              </p>
            )}
          </div>
        ))}
    </Card>
  );
}
