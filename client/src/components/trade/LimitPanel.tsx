"use client";

import { useState } from "react";
import { X } from "lucide-react";
import { cancelOrder, errText, fmtUsd, getOrders, placeOrder } from "@/lib/api";
import { useAsync, useSession } from "@/lib/session";
import type { MarketDef } from "@/lib/tokens";
import type { Order } from "@/lib/types";
import { Button, Notice, StatusPill } from "@/components/ui";
import { ExecutionProgress } from "@/components/Execution";
import { DelegationCard } from "@/components/automation/Delegation";
import { Field, inputCls } from "@/components/automation/ScheduleForm";
import { toastError } from "@/components/TxToast";
import { actionBtn, Row, SidePicker } from "./MarketPanel";

const DEC = /^\d+(\.\d+)?$/;

// Kuru margin-account limit order via POST /me/orders; `fund` deposits the notional to the trading balance first.
export function LimitPanel({ market, mid }: { market: MarketDef; mid: number | null }) {
  const { ready, authenticated, login, delegated } = useSession();
  const [side, setSide] = useState<"buy" | "sell">("buy");
  const [price, setPrice] = useState("");
  const [size, setSize] = useState("");
  const [postOnly, setPostOnly] = useState(true);
  const [fund, setFund] = useState(true);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [execId, setExecId] = useState<string | null>(null);
  const orders = useAsync(authenticated ? "orders" : null, getOrders);

  const notional = DEC.test(price) && DEC.test(size) ? Number(price) * Number(size) : null;
  const crosses = mid !== null && DEC.test(price) && (side === "buy" ? Number(price) >= mid : Number(price) <= mid);
  const reason = !ready
    ? "Loading…"
    : !delegated
      ? "Limit orders need automation enabled"
      : !DEC.test(price.trim()) || Number(price) <= 0
        ? "Enter a price"
        : !DEC.test(size.trim()) || Number(size) <= 0
          ? "Enter a size"
          : Number(size) < market.minSize
            ? `Min size ${market.minSize} ${market.symbol}`
            : null;

  async function submit() {
    setBusy(true);
    setErr(null);
    try {
      const r = await placeOrder({ market: market.symbol, side, price: price.trim(), size: size.trim(), postOnly, fund });
      setExecId(r.executionId);
    } catch (e) {
      setErr(errText(e));
    } finally {
      setBusy(false);
    }
  }

  const mine = (orders.data ?? []).filter((o) => o.market === market.symbol && (o.status === "open" || o.status === "pending"));

  return (
    <div className="space-y-4 p-5">
      <SidePicker id="lim-side" value={side} onChange={setSide} options={[["buy", "Buy"], ["sell", "Sell"]]} />
      <div className="grid grid-cols-2 gap-3">
        <Field label="Price (USDC)" hint={mid !== null ? <button onClick={() => setPrice(mid.toFixed(4))}>mid {fmtUsd(mid)}</button> : undefined}>
          <input className={inputCls} inputMode="decimal" placeholder="0.00" value={price} onChange={(e) => setPrice(e.target.value)} />
        </Field>
        <Field label={`Size (${market.symbol})`}>
          <input className={inputCls} inputMode="decimal" placeholder="0" value={size} onChange={(e) => setSize(e.target.value)} />
        </Field>
      </div>
      <div className="space-y-2 text-sm">
        <label className="flex items-center gap-2">
          <input type="checkbox" checked={postOnly} onChange={(e) => setPostOnly(e.target.checked)} className="accent-[var(--primary)]" />
          Post-only (maker)
        </label>
        <label className="flex items-center gap-2">
          <input type="checkbox" checked={fund} onChange={(e) => setFund(e.target.checked)} className="accent-[var(--primary)]" />
          Fund trading balance first ({side === "buy" ? "price × size USDC" : `size ${market.symbol}`})
        </label>
      </div>
      <dl className="space-y-1.5 px-1 text-xs">
        <Row label="Notional">{notional !== null ? fmtUsd(notional) : "—"}</Row>
        <Row label="Min size">{`${market.minSize} ${market.symbol}`}</Row>
      </dl>
      {crosses && postOnly && <Notice>Price crosses the mid; a post-only order may be rejected.</Notice>}
      {authenticated && !delegated && <DelegationCard compact />}
      {err && <Notice tone="bad">{err}</Notice>}
      {!authenticated && ready ? (
        <Button className={actionBtn} onClick={login}>
          Log in to trade
        </Button>
      ) : (
        <Button className={actionBtn} pending={busy} disabled={reason !== null} onClick={() => void submit()}>
          {busy ? "Submitting…" : (reason ?? `Place ${side} limit`)}
        </Button>
      )}
      {execId && <ExecutionProgress id={execId} onSettled={orders.reload} />}
      {authenticated && <OrderList orders={mine} onChange={orders.reload} />}
    </div>
  );
}

export function OrderList({ orders, onChange, showMarket }: { orders: Order[]; onChange: () => void; showMarket?: boolean }) {
  const [busy, setBusy] = useState<string | null>(null);
  async function cancel(id: string) {
    setBusy(id);
    try {
      await cancelOrder(id);
      onChange();
    } catch (e) {
      toastError(new Error(errText(e)));
    } finally {
      setBusy(null);
    }
  }
  if (!orders.length) return <p className="text-xs text-muted-foreground">No open limit orders.</p>;
  return (
    <ul className="divide-y divide-border rounded-2xl border border-border bg-card text-sm">
      {orders.map((o) => (
        <li key={o.id} className="flex items-center gap-3 px-4 py-2.5">
          <span className={o.side === "buy" ? "text-positive" : "text-destructive"}>{o.side}</span>
          {showMarket && <span className="font-medium">{o.market}</span>}
          <span className="tnum">
            {o.size} @ {fmtUsd(Number(o.price))}
          </span>
          <span className="ml-auto">
            <StatusPill status={o.status} />
          </span>
          <button
            aria-label="Cancel order"
            disabled={busy === o.id || !o.kuruOrderId}
            title={o.kuruOrderId ? "Cancel" : "Not live on Kuru yet"}
            onClick={() => void cancel(o.id)}
            className="rounded-md p-1 text-muted-foreground hover:text-destructive disabled:opacity-40"
          >
            <X className="size-4" />
          </button>
        </li>
      ))}
    </ul>
  );
}
