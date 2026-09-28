"use client";

import { useMemo } from "react";
import { motion } from "motion/react";
import { STAGGER_STEP, rise } from "@/components/motion";
import { TxState } from "@/components/table/TxState";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { Pill } from "@/components/ui/Pill";
import { Skeleton } from "@/components/ui/Skeleton";
import { errorMessage, fromWad, txTier, type Order } from "@/lib/api";
import { formatSize } from "@/lib/format";
import { marketIndex, normId } from "@/lib/rows";
import {
  useCancelOrder,
  useCancelPerplOrder,
  useExecutions,
  useMarkets,
  useOrders,
} from "@/lib/query";
import { SectionTitle } from "@/components/ui/SectionTitle";

// the executor answers with the live slot when the bind fails: the order changed,
// it is not a generic failure
const moved = (msg: string) => /slot \d+ (is empty|now holds)/.test(msg);

// kuru cancels by slot and the order in it; perpl by its own order id
const cancellable = (o: Order) =>
  !!o.order_id && (o.venue === "perpl" || o.slot_idx !== undefined);

export function OpenOrders({ venue }: { venue?: string }) {
  const orders = useOrders();
  const executions = useExecutions();
  const markets = useMarkets();
  const kuruCancel = useCancelOrder();
  const perplCancel = useCancelPerplOrder();
  const cancel = venue === "perpl" ? perplCancel : kuruCancel;

  const byId = useMemo(() => marketIndex(markets.data ?? []), [markets.data]);
  const resting = (orders.data?.orders ?? []).filter(
    (o) => o.state === "resting" && (!venue || o.venue === venue),
  );

  const error = cancel.error ? errorMessage(cancel.error) : null;

  return (
    <div className="space-y-4 p-4">
      <SectionTitle>Open orders</SectionTitle>

      {orders.isError ? (
        <ErrorState message={errorMessage(orders.error)} />
      ) : orders.isPending ? (
        <div className="space-y-2">
          <Skeleton className="h-16 rounded-2xl" />
          <Skeleton className="h-16 rounded-2xl" />
        </div>
      ) : resting.length === 0 ? (
        <EmptyState message="No orders are resting on the book." />
      ) : (
        <div className="space-y-2">
          {resting.map((o, i) => {
            const m = byId.get(normId(o.market_id));
            const tier = txTier(
              executions.data?.executions.find((e) => e.intent_key === o.intent_key),
            );
            const pending =
              (kuruCancel.isPending && kuruCancel.variables?.expected_order_id === o.order_id) ||
              (perplCancel.isPending && perplCancel.variables?.order_id === o.order_id);
            return (
              <motion.div
                key={o.intent_key}
                {...rise}
                transition={{ ...rise.transition, delay: i * STAGGER_STEP }}
              >
                <Card className="flex flex-wrap items-center gap-x-3 gap-y-2 bg-background/40 p-4">
                  <span className="font-medium">{m?.symbol ?? o.market_id}</span>
                  <Pill tone={o.side === "buy" ? "long" : "short"}>{o.side}</Pill>
                  {o.venue === "perpl" ? (
                    <Pill>order {o.order_id ?? "—"}</Pill>
                  ) : (
                    <Pill>slot {o.slot_idx ?? "—"}</Pill>
                  )}
                  {o.leverage ? <Pill>{o.leverage / 100}x</Pill> : null}
                  {o.tif && <Pill>{o.tif.toUpperCase()}</Pill>}
                  {o.post_only && <Pill>post only</Pill>}
                  {tier && <TxState tier={tier} />}
                  <span className="tnum ml-auto text-xs text-muted-foreground">
                    {o.limit_price && m ? formatSize(fromWad(o.limit_price, m.priceDecimals)) : "—"}
                    {" × "}
                    {o.quantity && m ? formatSize(fromWad(o.quantity, m.sizeDecimals)) : "—"}
                  </span>
                  <Button
                    variant="ghost"
                    pending={pending}
                    disabled={!cancellable(o)}
                    onClick={() =>
                      o.venue === "perpl"
                        ? perplCancel.mutate({ market_id: o.market_id, order_id: o.order_id! })
                        : kuruCancel.mutate({
                            market_id: o.market_id,
                            slot_idx: o.slot_idx!,
                            expected_order_id: o.order_id!,
                          })
                    }
                  >
                    Cancel
                  </Button>
                  {!cancellable(o) && (
                    <p className="w-full text-xs text-muted-foreground">
                      This order has no order id recorded, and a cancel has to name it, so it
                      can only be cancelled from the venue.
                    </p>
                  )}
                </Card>
              </motion.div>
            );
          })}
        </div>
      )}

      {error && (
        <ErrorState
          message={moved(error) ? `That order already changed on the book: ${error}` : error}
        />
      )}

      {cancel.data?.state === "not_canceled" && (
        <ErrorState message="The cancel transaction confirmed but the order is still on the book, so it is still live." />
      )}
    </div>
  );
}
