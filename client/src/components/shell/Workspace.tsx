"use client";

import { useEffect, useMemo, useState } from "react";
import { AccountPanel } from "@/components/account/AccountPanel";
import { ChartPanel } from "@/components/chart/ChartPanel";
import { MarketHeader } from "@/components/market/MarketHeader";
import { PositionsPanel } from "@/components/table/PositionsPanel";
import { OpenOrders } from "@/components/ticket/OpenOrders";
import { OrderTicket } from "@/components/ticket/OrderTicket";
import { Card } from "@/components/ui/Card";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { errorMessage, fromWad, type Candle, type IndexFrame, type Tick } from "@/lib/api";
import {
  useCandles,
  useExecutions,
  useMarkets,
  useMarks,
  useOrders,
  usePortfolio,
} from "@/lib/query";
import { marketIndex, orderRows, positionRows } from "@/lib/rows";
import { useTopic } from "@/lib/ws";
import { useSession } from "@/lib/session";

const DAY = 86_400;

export function Workspace({
  venue,
  idPrefix,
  emptyMessage = "No markets are listed here yet.",
}: {
  venue?: string;
  idPrefix?: string;
  emptyMessage?: string;
}) {
  const { authenticated } = useSession();
  const markets = useMarkets();
  const [symbol, setSymbol] = useState<string | null>(null);

  const listed = useMemo(
    () =>
      (markets.data ?? []).filter(
        (m) =>
          (!venue || m.venue === venue) &&
          (!idPrefix || m.id.toLowerCase().startsWith(idPrefix)),
      ),
    [markets.data, venue, idPrefix],
  );

  useEffect(() => {
    if (listed.length && !listed.some((m) => m.symbol === symbol)) {
      setSymbol(listed[0].symbol);
    }
  }, [listed, symbol]);

  const market = listed.find((m) => m.symbol === symbol) ?? null;
  const id = market?.id ?? null;

  const marks = useMarks(id ? [id] : []);
  const tick = useTopic<Tick>(market ? `tick.${market.venue}.${market.id.toLowerCase()}` : null);
  const index = useTopic<IndexFrame>(id ? `index.${id.toLowerCase()}` : null);

  const day = useMemo(() => {
    const to = Math.floor(Date.now() / 1000);
    return { from: to - DAY, to };
  }, []);
  const hourly = useCandles(id, 3_600, day.from, day.to);

  const portfolio = usePortfolio();
  const orders = useOrders();
  const executions = useExecutions();

  const stats = useMemo(() => summarize(hourly.data ?? []), [hourly.data]);
  const markPrice = marks.data?.marks.find((m) => m.market_id === id)?.price;
  const price = tick.data
    ? fromWad(tick.data.price)
    : markPrice
      ? Number(markPrice)
      : market?.last
        ? fromWad(market.last)
        : undefined;

  const byId = useMemo(() => marketIndex(markets.data ?? []), [markets.data]);
  const rows = useMemo(
    () => [
      ...positionRows(portfolio.data?.positions ?? [], byId),
      ...orderRows(orders.data?.orders ?? [], executions.data?.executions ?? [], byId),
    ],
    [portfolio.data, orders.data, executions.data, byId],
  );

  const header = market
    ? {
        symbol: market.symbol,
        price,
        markPrice: markPrice ? Number(markPrice) : undefined,
        indexPrice: index.data ? fromWad(index.data.value) : undefined,
        changePct: stats?.changePct,
        high24h: stats?.high,
        low24h: stats?.low,
        volume24h: stats?.volume,
      }
    : null;

  if (markets.isError) {
    return (
      <div className="p-4">
        <ErrorState message={errorMessage(markets.error)} />
      </div>
    );
  }
  if (!markets.isPending && listed.length === 0) {
    return (
      <div className="p-4">
        <Card className="p-4">
          <EmptyState message={emptyMessage} />
        </Card>
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 gap-4 overflow-hidden p-4">
      <main className="flex min-w-0 flex-1 flex-col gap-4 overflow-y-auto">
        <Card>
          <MarketHeader
            market={header}
            markets={listed.map((m) => ({ symbol: m.symbol }))}
            onSelect={setSymbol}
          />
        </Card>
        <Card className="flex min-h-[420px] flex-col">
          <ChartPanel market={market} />
        </Card>
        {authenticated && market?.venue === "kuru" && (
          <Card>
            <OpenOrders venue="kuru" />
          </Card>
        )}
        <Card>
          {authenticated ? (
            <PositionsPanel
              positions={rows}
              loading={portfolio.isPending || orders.isPending}
              error={
                portfolio.isError
                  ? errorMessage(portfolio.error)
                  : orders.isError
                    ? errorMessage(orders.error)
                    : null
              }
              degraded={portfolio.data?.degraded ?? []}
            />
          ) : (
            <EmptyState message="Sign in to see your positions and orders." />
          )}
        </Card>
      </main>

      <aside className="flex w-[var(--ticket-width)] shrink-0 flex-col gap-4 overflow-y-auto">
        {/* kuru custody is a step of its own: funds live in AccountCore, not the wallet */}
        {market?.venue === "kuru" && <AccountPanel market={market} />}
        <OrderTicket market={market} />
      </aside>
    </div>
  );
}

// 24h figures come from hourly candles; volume is base size on kuru, so it is
// valued at each bar's close to read as notional
function summarize(candles: Candle[]) {
  if (!candles.length) return undefined;
  let high = -Infinity;
  let low = Infinity;
  let volume = 0;
  for (const c of candles) {
    high = Math.max(high, fromWad(c.high));
    low = Math.min(low, fromWad(c.low));
    volume += fromWad(c.volume) * fromWad(c.close);
  }
  const open = fromWad(candles[0].open);
  const close = fromWad(candles[candles.length - 1].close);
  return {
    high,
    low,
    volume,
    changePct: open === 0 ? undefined : (close / open - 1) * 100,
  };
}
