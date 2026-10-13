"use client";

import Link from "next/link";
import { parseUnits } from "viem";
import { useEffect, useState } from "react";
import { errText, fmtNum, getMyStrategies, getOrders, getPositions, getStrategies } from "@/lib/api";
import { useHoldings, useTickers } from "@/lib/hooks";
import { useAsync, useSession } from "@/lib/session";
import { tokenBySymbol } from "@/lib/tokens";
import { Button, ErrorBox, Panel, Skeleton } from "@/components/ui";
import { Holdings, type Row } from "@/components/portfolio/Holdings";
import { ValueChart } from "@/components/portfolio/ValueChart";
import { TradingBalance } from "@/components/portfolio/TradingBalance";
import { EarningsCard } from "@/components/portfolio/Earnings";
import { DelegationCard } from "@/components/automation/Delegation";
import { ExecutionLog, ScheduleList } from "@/components/automation/ScheduleList";
import { OrderList } from "@/components/trade/LimitPanel";
import { PositionCard, PRICE_OF, STABLES } from "@/components/portfolio/PositionCard";

export default function PortfolioPage() {
  const { ready, authenticated, login, address } = useSession();
  const on = ready && authenticated;
  const holdings = useHoldings();
  const { tickers, loading: pxLoading } = useTickers();
  const positions = useAsync(on ? "positions" : null, getPositions);
  const orders = useAsync(on ? "orders" : null, getOrders);
  const baskets = useAsync("strategies", () => getStrategies());
  const mine = useAsync(on ? "my-strategies" : null, getMyStrategies);

  if (ready && !authenticated) {
    return (
      <main className="w-full px-4 pb-24 pt-16 md:px-6">
        <Panel className="mx-auto w-full max-w-md p-8 text-center">
          <h1 className="text-lg font-medium">Your portfolio</h1>
          <p className="mt-2 text-sm text-muted-foreground">Log in to see your wallet, baskets, orders and automations.</p>
          <Button className="mt-6 w-full" onClick={login}>
            Log in
          </Button>
        </Panel>
      </main>
    );
  }

  const priceOf = (sym: string) => (STABLES.has(sym) ? 1 : (tickers[PRICE_OF[sym] ?? sym]?.price ?? null));
  const h = holdings.data;
  const raw = (sym: string) => (h ? BigInt(h.tokens.find((t) => t.symbol === sym)?.raw ?? 0) : undefined);
  const rows: Row[] = [];
  for (const t of h?.tokens ?? []) {
    if (t.symbol === "MON" || t.symbol === "USDC" || Number(t.amount) === 0) continue;
    const price = priceOf(t.symbol);
    rows.push({
      key: t.symbol,
      symbol: t.symbol,
      name: tokenBySymbol(t.symbol)?.name ?? t.symbol,
      cls: STABLES.has(t.symbol) ? "Stablecoin" : "Crypto",
      icons: [t.symbol],
      href: tickers[t.symbol] ? `/trade/${t.symbol}` : undefined,
      price,
      balance: fmtNum(Number(t.amount), 6),
      value: price === null ? null : Number(t.amount) * price,
    });
  }
  const tokensUsd = (h?.tokens ?? []).reduce((s, t) => s + Number(t.amount) * (priceOf(t.symbol) ?? 0), 0);
  const open = (positions.data ?? []).filter((p) => p.status !== "closed");
  const posUsd = open.reduce((s, p) => s + Number(p.principalUsdc), 0);
  const total = tokensUsd + posUsd;
  const legs = (h?.tokens ?? [])
    .map((t) => ({ market: PRICE_OF[t.symbol] ?? t.symbol, amount: Number(t.amount) }))
    .filter((l) => l.amount > 0 && tickers[l.market]);
  const loading = !ready || holdings.loading || pxLoading;
  const mon = raw("MON");
  const names = Object.fromEntries([...(baskets.data ?? []), ...(mine.data ?? [])].map((b) => [b.id, b.name]));
  const openOrders = (orders.data ?? []).filter((o) => o.status === "open" || o.status === "pending");

  return (
    <main className="w-full px-4 pb-24 pt-8 md:px-6">
      <div className="mx-auto w-full space-y-6">
        <header className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex min-w-0 items-center gap-3">
            <h1 className="truncate text-xl font-medium sm:text-2xl">
              <span className="text-muted-foreground">Welcome, </span>
              {address ? `${address.slice(0, 6)}…${address.slice(-4)}` : <Skeleton className="inline-block h-6 w-32 align-middle" />}
            </h1>
          </div>
          {ready && <Clock />}
        </header>

        <ValueChart legs={legs} flat={total - legs.reduce((s, l) => s + l.amount * (priceOf(l.market) ?? 0), 0)} total={total} loading={loading} />

        {holdings.error && <ErrorBox message={`Wallet balances unavailable: ${errText(new Error(holdings.error))}`} />}

        <Holdings rows={rows} loading={loading} usdc={raw("USDC")} mon={mon} lowGas={mon !== undefined && mon < LOW_GAS} baskets={baskets.data} />

        <section className="space-y-3">
          <div className="flex items-center justify-between px-1">
            <h2 className="text-lg font-medium">My baskets</h2>
            <Link href="/baskets" className="text-sm text-muted-foreground hover:text-foreground">
              Explore baskets →
            </Link>
          </div>
          {positions.error && <ErrorBox message={errText(new Error(positions.error))} />}
          {positions.loading && !positions.data ? (
            <Skeleton className="h-28 w-full rounded-3xl" />
          ) : (positions.data ?? []).length === 0 ? (
            <Panel className="p-5 text-sm text-muted-foreground">No baskets yet.</Panel>
          ) : (
            <div className="grid gap-3 md:grid-cols-2">
              {(positions.data ?? []).map((p) => (
                <PositionCard key={p.id} p={p} priceOf={priceOf} onChange={positions.reload} />
              ))}
            </div>
          )}
        </section>

        <section className="space-y-3">
          <h2 className="px-1 text-lg font-medium">Trading balance</h2>
          <TradingBalance />
        </section>

        <EarningsCard />

        <section className="space-y-3">
          <h2 className="px-1 text-lg font-medium">Open limit orders</h2>
          <Panel className="p-5">{orders.loading && !orders.data ? <Skeleton className="h-6 w-1/2" /> : <OrderList orders={openOrders} onChange={orders.reload} showMarket />}</Panel>
        </section>

        <section className="space-y-3">
          <h2 className="px-1 text-lg font-medium">Automation</h2>
          <DelegationCard />
          <ScheduleList names={names} />
          <ExecutionLog />
        </section>
      </div>
    </main>
  );
}

const LOW_GAS = parseUnits("0.1", 18);

function useNow() {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);
  return now;
}

function Clock() {
  const now = useNow();
  return (
    <time className="tnum text-sm text-muted-foreground sm:text-base" dateTime={new Date(now).toISOString()}>
      {new Date(now).toLocaleString("en-GB", { day: "numeric", month: "long", year: "numeric", hour: "2-digit", minute: "2-digit", second: "2-digit" })}
    </time>
  );
}
