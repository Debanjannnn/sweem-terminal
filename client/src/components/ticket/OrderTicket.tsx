"use client";

import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Dropdown } from "@/components/ui/Dropdown";
import { ErrorState } from "@/components/ui/ErrorState";
import {
  CHAIN_ID,
  errorMessage,
  fromWad,
  toUnits,
  toWad,
  wadToUnits,
  type Market,
} from "@/lib/api";
import { formatPct, formatSize, formatUsd } from "@/lib/format";
import { useCreateOrder, useDelegation, useQuotePreview } from "@/lib/query";
import { useSession } from "@/lib/session";
import { AmountInput } from "./AmountInput";
import { OrderTypeTabs, type OrderType } from "./OrderTypeTabs";
import { SideToggle, type Side } from "./SideToggle";
import { SummaryRows } from "./SummaryRows";

const MARGIN_MODES = [
  { value: "isolated", label: "Isolated" },
  { value: "cross", label: "Cross" },
] as const;

export function OrderTicket({ market }: { market?: Market | null }) {
  const [margin, setMargin] = useState<"isolated" | "cross">("isolated");
  const [side, setSide] = useState<Side>("long");
  const [type, setType] = useState<OrderType>("market");
  const [leverage, setLeverage] = useState(10);
  const [trigger, setTrigger] = useState("");
  const [price, setPrice] = useState("");
  const [amount, setAmount] = useState("");

  const { authenticated, login } = useSession();
  const delegation = useDelegation();
  const create = useCreateOrder();
  const key = useRef(crypto.randomUUID());

  const base = market?.base ?? "";
  const quoteSymbol = market?.quote ?? "USDC";
  const apiSide = side === "long" ? "buy" : "sell";
  const sizeWad = amount && Number(amount) > 0 ? toWad(amount) : "";
  const quote = useQuotePreview(market?.id ?? null, apiSide, sizeWad);

  // a new attempt is a new order; a retry of the same one keeps its key
  useEffect(() => {
    key.current = crypto.randomUUID();
  }, [market?.id, side, type, amount]);

  const perp = market?.kind === "perp";
  const notionalWad = quote.data?.notional;
  const submitError = create.error ? errorMessage(create.error) : null;

  // a buy spends quote tokens, so its size comes from the quoted notional
  const size =
    !market || !sizeWad
      ? null
      : apiSide === "sell"
        ? toUnits(amount, market.baseDecimals)
        : notionalWad
          ? wadToUnits(notionalWad, market.quoteDecimals)
          : null;

  const blocked =
    !market
      ? "Pick a market before placing an order."
      : type === "stop"
        ? "This terminal routes market and limit orders only, so a stop cannot be submitted yet."
        : !sizeWad
          ? null
          : apiSide === "buy" && !size
            ? quote.isError
              ? errorMessage(quote.error)
              : "A buy is sized from a live quote, and this market has not been quoted yet."
            : null;

  const submit = async () => {
    if (!market || !size) return;
    try {
      const res = await create.mutateAsync({
        market_id: market.id,
        venue: market.venue,
        side: apiSide,
        order_type: type === "limit" ? "limit" : "market",
        size,
        limit_price:
          type === "limit" && price ? toUnits(price, market.quoteDecimals) : undefined,
        chain_id: CHAIN_ID,
        idempotency_key: key.current,
      });
      toast.success(`Order ${res.state}`, { description: res.tx_hash ?? res.intent_key });
      setAmount("");
    } catch {
      // the message renders under the button; a toast would hide the 412 sentence
    }
  };

  return (
    <div className="flex flex-col gap-4">
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

        <SideToggle value={side} onChange={setSide} />
        <OrderTypeTabs value={type} onChange={setType} />

        {type === "stop" && (
          <AmountInput
            label="Trigger price"
            value={trigger}
            onChange={setTrigger}
            suffix={quoteSymbol}
          />
        )}
        {type !== "market" && (
          <AmountInput
            label="Order price"
            value={price}
            onChange={setPrice}
            suffix={quoteSymbol}
          />
        )}

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

        <AmountInput
          label="Order size"
          value={amount}
          onChange={setAmount}
          suffix={base || "size"}
        />

        <div className="h-px bg-border" />

        <SummaryRows
          rows={[
            {
              label: "Average price",
              value: quote.data ? formatUsd(fromWad(quote.data.avgPrice)) : null,
            },
            {
              label: "Price impact",
              value: quote.data ? formatPct(quote.data.impactBps / 100) : null,
            },
            {
              label: `Spends (${apiSide === "buy" ? quoteSymbol : base || "base"})`,
              value: quote.data
                ? apiSide === "buy"
                  ? formatUsd(fromWad(quote.data.notional))
                  : formatSize(fromWad(quote.data.filled))
                : null,
            },
          ]}
        />

        {quote.data?.partial && (
          <ErrorState message="The book is too thin to fill this size, so the quote is partial." />
        )}

        {authenticated ? (
          <Button
            className="w-full"
            onClick={submit}
            pending={create.isPending}
            disabled={!size || !!blocked}
          >
            Place Order
          </Button>
        ) : (
          <Button className="w-full" onClick={login}>
            Sign in to trade
          </Button>
        )}

        {blocked && <ErrorState message={blocked} />}
        {submitError && <ErrorState message={submitError} />}
      </Card>

      <Card className="space-y-3 p-4">
        <h2 className="text-xs uppercase tracking-wide text-muted-foreground">
          Delegation limits
        </h2>
        <SummaryRows
          rows={[
            {
              label: "Per-order cap",
              value: delegation.data?.max_notional_per_slice ?? null,
            },
            { label: "Total cap", value: delegation.data?.max_notional_total ?? null },
            {
              label: "Expires",
              value: delegation.data
                ? new Date(delegation.data.expires_at).toLocaleString()
                : null,
            },
          ]}
        />
        {!delegation.data && authenticated && (
          <p className="text-xs text-muted-foreground">
            No signer is delegated, so scheduled strategies cannot run. Manual orders still
            need one: the backend refuses to execute without it.
          </p>
        )}
      </Card>
    </div>
  );
}
