"use client";

import { useMemo } from "react";
import { Card } from "@/components/ui/Card";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { Skeleton } from "@/components/ui/Skeleton";
import { PositionsPanel } from "@/components/table/PositionsPanel";
import { TxState } from "@/components/table/TxState";
import { errorMessage, txTier } from "@/lib/api";
import { truncateAddress } from "@/lib/format";
import { useExecutions, useMarkets, useOrders, usePortfolio } from "@/lib/query";
import { marketIndex, orderRows, positionRows } from "@/lib/rows";
import { useSession } from "@/lib/session";

export default function Page() {
  const { authenticated, ready } = useSession();
  const portfolio = usePortfolio();
  const orders = useOrders();
  const executions = useExecutions();
  const markets = useMarkets();

  const byId = useMemo(() => marketIndex(markets.data ?? []), [markets.data]);
  const rows = useMemo(
    () => [
      ...positionRows(portfolio.data?.positions ?? [], byId),
      ...orderRows(orders.data?.orders ?? [], executions.data?.executions ?? [], byId),
    ],
    [portfolio.data, orders.data, executions.data, byId],
  );

  if (ready && !authenticated) {
    return (
      <div className="min-h-0 flex-1 overflow-y-auto p-4">
        <Card className="p-4">
          <h1 className="text-sm font-medium">Portfolio</h1>
          <EmptyState message="Connect a wallet to see your portfolio." />
        </Card>
      </div>
    );
  }

  return (
    <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-4">
      <Card className="space-y-2 p-4">
        <h1 className="text-sm font-medium">Portfolio</h1>
        {portfolio.isError ? (
          <ErrorState message={errorMessage(portfolio.error)} />
        ) : portfolio.isPending ? (
          <Skeleton className="h-4 w-48" />
        ) : (
          <p className="tnum text-xs text-muted-foreground">
            {truncateAddress(portfolio.data.wallet_address)} · updated{" "}
            {new Date(portfolio.data.updated_at).toLocaleTimeString()}
          </p>
        )}
      </Card>

      <Card>
        <PositionsPanel
          positions={rows}
          loading={portfolio.isPending || orders.isPending}
          error={portfolio.isError ? errorMessage(portfolio.error) : null}
          degraded={portfolio.data?.degraded ?? []}
        />
      </Card>

      <Card className="space-y-3 p-4">
        <h2 className="text-sm font-medium">Executions</h2>
        {executions.isError ? (
          <ErrorState message={errorMessage(executions.error)} />
        ) : executions.isPending ? (
          <div className="space-y-2">
            <Skeleton className="h-10 rounded-xl" />
            <Skeleton className="h-10 rounded-xl" />
          </div>
        ) : executions.data.executions.length === 0 ? (
          <EmptyState message="Nothing has been executed for this account yet." />
        ) : (
          <ul className="space-y-2">
            {executions.data.executions.map((e) => (
              <li
                key={e.intent_key}
                className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-xl border border-border p-3 text-xs"
              >
                <TxState tier={txTier(e)!} />
                <span className="tnum text-muted-foreground">
                  {e.tx_hash ? truncateAddress(e.tx_hash) : e.intent_key.slice(0, 12)}
                </span>
                <span className="tnum ml-auto text-muted-foreground">
                  {new Date(e.submitted_at).toLocaleString()}
                </span>
                {e.error && <span className="w-full text-destructive">{e.error}</span>}
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  );
}
