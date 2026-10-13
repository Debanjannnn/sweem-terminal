"use client";

import Link from "next/link";
import { formatUnits } from "viem";
import { useState } from "react";
import { errText, fmtTime, fmtUsd, getBaskets, getMyBaskets, getOrders, getPositions, withdrawPosition } from "@/lib/api";
import { useHoldings, useTickers } from "@/lib/hooks";
import { useAsync, useSession } from "@/lib/session";
import { fmtUnits, tokenBySymbol } from "@/lib/tokens";
import type { Position } from "@/lib/types";
import { Button, ErrorBox, Notice, Panel, Skeleton, StatusPill } from "@/components/ui";
import { TokenIcon } from "@/components/TokenIcon";
import { IconStack } from "@/components/invest/BasketCard";
import { LOW_GAS, useNow } from "@/components/portfolio/Wallet";
import { Holdings, type Row } from "@/components/portfolio/Holdings";
import { ValueChart } from "@/components/portfolio/ValueChart";
import { TradingBalance } from "@/components/portfolio/TradingBalance";
import { EarningsCard } from "@/components/portfolio/Earnings";
import { DelegationCard } from "@/components/automation/Delegation";
import { ExecutionLog, ScheduleList } from "@/components/automation/ScheduleList";
import { ScheduleForm } from "@/components/automation/ScheduleForm";
import { OrderList } from "@/components/trade/LimitPanel";
import { ExecutionProgress } from "@/components/Execution";
import { KIND_LABEL, venueName, protocolOf } from "@/components/earn/sources";
import { ProtocolIcon } from "@/components/ProtocolIcon";

const STABLES = new Set(["USDC", "AUSD", "USDT0"]);
const PRICE_OF: Record<string, string> = { WMON: "MON" };

export default function PortfolioPage() {
  const { ready, authenticated, login, address } = useSession();
  const on = ready && authenticated;
  const holdings = useHoldings();
  const { tickers, loading: pxLoading } = useTickers();
  const positions = useAsync(on ? "positions" : null, getPositions);
  const orders = useAsync(on ? "orders" : null, getOrders);
  const baskets = useAsync("baskets", () => getBaskets());
  const mine = useAsync(on ? "my-baskets" : null, getMyBaskets);

  if (ready && !authenticated) {
    return (
      <main className="w-full px-4 pb-24 pt-16 md:px-6">
        <Panel className="mx-auto w-full max-w-md p-8 text-center">
          <h1 className="text-lg font-medium">Your portfolio</h1>
          <p className="mt-2 text-sm text-muted-foreground">Log in to see your wallet, strategies, orders and automations.</p>
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
      balance: Number(t.amount).toLocaleString("en-US", { maximumFractionDigits: 6 }),
      value: price === null ? null : Number(t.amount) * price,
    });
  }
  for (const b of h?.baskets ?? []) {
    const value = b.legs.reduce<number | null>((s, l) => (s === null || priceOf(l.market) === null ? null : s + Number(l.amount) * priceOf(l.market)!), 0);
    rows.push({
      key: b.basketId,
      symbol: b.name,
      name: "Basket · tokens in your wallet",
      cls: "Basket",
      icons: b.legs.map((l) => l.market),
      href: `/baskets/${b.basketId}`,
      price: null,
      balance: b.legs.map((l) => `${Number(l.amount).toLocaleString("en-US", { maximumFractionDigits: 4 })} ${l.market}`).join(" + "),
      value,
    });
  }
  // basket legs are also wallet tokens; count tokens once, baskets are an attribution view
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
            <Avatar address={address ?? ""} />
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
            <h2 className="text-lg font-medium">Earn positions</h2>
            <Link href="/earn" className="text-sm text-muted-foreground hover:text-foreground">
              Explore strategies →
            </Link>
          </div>
          {positions.error && <ErrorBox message={errText(new Error(positions.error))} />}
          {positions.loading && !positions.data ? (
            <Skeleton className="h-28 w-full rounded-3xl" />
          ) : (positions.data ?? []).length === 0 ? (
            <Panel className="p-5 text-sm text-muted-foreground">No strategy positions yet.</Panel>
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

// principal is what went in; per-source amounts are the atoms supplied at deploy (venue yield accrues on-venue)
function PositionCard({ p, priceOf, onChange }: { p: Position; priceOf: (sym: string) => number | null; onChange: () => void }) {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [execId, setExecId] = useState<string | null>(null);
  const [auto, setAuto] = useState(false);
  // legs grouped by sleeve token (old rows: no token = position asset); each group marked at its own price
  const groups = Object.entries(Object.groupBy(p.legs, (l) => l.token ?? p.asset)).map(([token, legs = []]) => ({ token, legs, dec: tokenBySymbol(token)?.decimals ?? 18 }));
  // ponytail: basis AUSD collateral counted at $1, hedge PnL not marked (~0); read venue balances via RPC if exact value matters
  const value = groups.reduce<number | null>((v, g) => {
    const px = priceOf(g.token);
    if (v === null || px === null) return null;
    return g.legs.reduce((s, l) => s + Number(formatUnits(BigInt(l.asset || 0), g.dec)) * px + (l.collateral ? Number(formatUnits(BigInt(l.collateral), 6)) : 0), v);
  }, 0);
  const pnl = value !== null ? value - Number(p.principalUsdc) : null;

  async function withdraw() {
    setBusy(true);
    setErr(null);
    try {
      setExecId((await withdrawPosition(p.id)).executionId);
    } catch (e) {
      setErr(errText(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Panel className="space-y-3 p-5">
      <div className="flex items-center gap-3">
        <IconStack symbols={groups.map((g) => g.token)} size={28} ring="ring-card" />
        <Link href={`/earn/${p.strategyId}`} className="truncate font-medium hover:underline">
          {p.strategyName}
        </Link>
        <span className="ml-auto">
          <StatusPill status={p.status} />
        </span>
      </div>
      <dl className="tnum grid grid-cols-3 gap-3 text-sm">
        <div>
          <dt className="text-xs text-muted-foreground">Principal</dt>
          <dd>{fmtUsd(Number(p.principalUsdc))}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">Value (spot)</dt>
          <dd>{value !== null ? fmtUsd(value) : "-"}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">PnL</dt>
          <dd className={pnl === null ? "" : pnl >= 0 ? "text-positive" : "text-destructive"}>{pnl !== null ? `${pnl >= 0 ? "+" : ""}${fmtUsd(pnl)}` : "-"}</dd>
        </div>
      </dl>
      <ul className="space-y-1 text-xs">
        {groups.flatMap((g) =>
          g.legs.map((l, i) => (
            <li key={`${g.token}${i}`} className="flex items-center gap-2">
              {groups.length > 1 && <TokenIcon symbol={g.token} size={14} />}
              <span className="rounded-md bg-secondary/60 px-1.5 py-0.5 text-[10px] text-muted-foreground">{KIND_LABEL[l.kind as keyof typeof KIND_LABEL] ?? l.kind}</span>
              <ProtocolIcon protocol={protocolOf(l.venue)} size={14} />
              {venueName(l.venue)}
              <span className="tnum ml-auto text-muted-foreground">
                {fmtUnits(l.asset || "0", g.dec)} {g.token}
                {l.entryPx ? ` · entry ${fmtUsd(Number(l.entryPx))}` : ""}
              </span>
            </li>
          )),
        )}
      </ul>
      <p className="text-[11px] text-muted-foreground">
        Opened {fmtTime(p.openedAt)}
        {p.lastRebalanceAt ? ` · rebalanced ${fmtTime(p.lastRebalanceAt)}` : ""}. Value marks supplied tokens at spot; lending/staking yield accrues on-venue.
      </p>
      {p.legs.some((l) => l.kind === "lp") && <Notice>ActiveVault leg: 4-day lock, withdraw may fail inside the lock.</Notice>}
      {err && <Notice tone="bad">{err}</Notice>}
      {p.status === "open" && (
        <div className="flex gap-2">
          <Button variant="ghost" pending={busy} onClick={() => void withdraw()}>
            Withdraw to USDC
          </Button>
          <Button variant="ghost" onClick={() => setAuto((a) => !a)}>
            {auto ? "Hide automation" : "TP / SL / Rebalance"}
          </Button>
        </div>
      )}
      {auto && <ScheduleForm target={{ type: "position", id: p.id }} current={value} onCreated={() => setAuto(false)} />}
      {execId && <ExecutionProgress id={execId} onSettled={onChange} />}
    </Panel>
  );
}

function Clock() {
  const now = useNow();
  return (
    <time className="tnum text-sm text-muted-foreground sm:text-base" dateTime={new Date(now).toISOString()}>
      {new Date(now).toLocaleString("en-GB", { day: "numeric", month: "long", year: "numeric", hour: "2-digit", minute: "2-digit", second: "2-digit" })}
    </time>
  );
}

// deterministic gradient blob from the address
function Avatar({ address }: { address: string }) {
  const h = parseInt(address.slice(2, 8) || "0", 16) % 360;
  return (
    <span
      aria-hidden
      className="size-10 shrink-0 rounded-full ring-1 ring-border"
      style={{ background: `radial-gradient(circle at 30% 30%, hsl(${h} 90% 70%), hsl(${(h + 60) % 360} 80% 45%) 60%, hsl(${(h + 120) % 360} 70% 25%))` }}
    />
  );
}
