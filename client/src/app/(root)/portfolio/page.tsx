"use client";

import Link from "next/link";
import { ExternalLink } from "lucide-react";
import { formatUnits, zeroAddress } from "viem";
import { fmtTime, getBaskets, getBook, getHistory, getPositions, shortHash } from "@/lib/api";
import { useAsync, useSession } from "@/lib/session";
import { SHARE_DECIMALS, txUrl, USDC_DECIMALS } from "@/lib/config";
import { fmtUnits } from "@/lib/tokens";
import { Button, ErrorBox, Panel, Skeleton } from "@/components/ui";
import { cn } from "@/lib/utils";
import { FaucetCard, LOW_GAS, useBalances, useNow } from "@/components/portfolio/Wallet";
import { pnlByBasket } from "@/components/portfolio/pnl";
import { Holdings, type Row } from "@/components/portfolio/Holdings";
import { ValueChart } from "@/components/portfolio/ValueChart";
import { symbolOf, useMarkets } from "@/components/invest/extras";
import { DelegationCard } from "@/components/automation/Delegation";
import { ScheduleList } from "@/components/automation/ScheduleList";

const usd = (raw: bigint) => `${raw < BigInt(0) ? "-" : ""}$${fmtUnits(raw < BigInt(0) ? -raw : raw, USDC_DECIMALS, 2)}`;
const STABLES = new Set(["USDC"]);

export default function PortfolioPage() {
  const { ready, authenticated, login, address } = useSession();
  const on = ready && authenticated;
  const positions = useAsync(on ? "positions" : null, getPositions);
  const history = useAsync(on ? "history" : null, getHistory);
  const baskets = useAsync("baskets", () => getBaskets());
  const balances = useBalances(on ? address : undefined);
  const markets = useMarkets();
  // token address → book mid in USDC; ponytail: one book fetch per market, fine for a handful of markets
  const prices = useAsync(on && markets.data ? "pf:prices" : null, async () => {
    const list = markets.data ?? [];
    const books = await Promise.allSettled(list.map((m) => getBook(m.market)));
    const out = new Map<string, number>();
    list.forEach((m, i) => {
      const r = books[i];
      if (r.status !== "fulfilled") return;
      const px = [r.value.bids[0]?.[0], r.value.asks[0]?.[0]].filter(Boolean).map(Number);
      if (px.length) out.set(m.base, px.reduce((a, b) => a + b) / px.length);
    });
    return out;
  });

  if (ready && !authenticated) {
    return (
      <main className="w-full px-4 pb-24 pt-16">
        <Panel className="mx-auto w-full max-w-md p-8 text-center">
          <h1 className="text-lg font-medium">Your portfolio</h1>
          <p className="mt-2 text-sm text-muted-foreground">Log in to see your holdings, baskets and automations.</p>
          <Button className="mt-6 w-full" onClick={login}>
            Log in
          </Button>
        </Panel>
      </main>
    );
  }

  const loading = !ready || positions.loading || balances.loading || (prices.loading && !prices.data);
  const pnl = pnlByBasket(history.data ?? [], positions.data ?? []);
  const nameOf = (addr: string) => baskets.data?.find((b) => b.address === addr);
  const activity = [...(history.data ?? [])].sort((a, b) => b.ts.localeCompare(a.ts)).slice(0, 25);
  const bal = (sym: string) => balances.data?.find(([t]) => t.symbol === sym)?.[1];
  const mon = bal("MON");

  const rows: Row[] = [];
  for (const [t, raw] of balances.data ?? []) {
    if (t.symbol === "MON" || raw === BigInt(0)) continue;
    const stable = STABLES.has(t.symbol);
    const price = stable ? 1 : (prices.data?.get(t.address) ?? null);
    rows.push({
      key: t.address,
      symbol: t.symbol,
      name: t.name,
      cls: stable ? "Stablecoin" : "Crypto",
      icons: [t.symbol],
      price,
      balance: fmtUnits(raw, t.decimals, 4),
      value: price === null ? null : Number(formatUnits(raw, t.decimals)) * price,
    });
  }
  for (const p of positions.data ?? []) {
    if (BigInt(p.sharesRaw) === BigInt(0)) continue;
    const b = nameOf(p.basket);
    const r = pnl.get(p.basket);
    rows.push({
      key: p.basket,
      symbol: b?.symbol ?? shortHash(p.basket),
      name: b?.name ?? "Basket",
      cls: "Basket",
      icons: b ? b.components.map((c) => symbolOf(c.market, markets.data)) : [],
      href: `/baskets/${p.basket}`,
      price: p.navPerShare ? Number(p.navPerShare) : null,
      balance: fmtUnits(p.sharesRaw, SHARE_DECIMALS, 4),
      value: p.valueUsdc ? Number(p.valueUsdc) : null,
      note: r && r.costRaw !== BigInt(0) && (
        <div className={cn("text-xs", r.pnlRaw >= BigInt(0) ? "text-positive" : "text-destructive")}>
          {r.pnlRaw > BigInt(0) ? "+" : ""}
          {usd(r.pnlRaw)} PnL
        </div>
      ),
    });
  }
  const monPx = prices.data?.get(zeroAddress) ?? 0;
  const total = rows.reduce((s, r) => s + (r.value ?? 0), 0) + (mon ? Number(formatUnits(mon, 18)) * monPx : 0);

  return (
    <main className="w-full px-4 pb-24 pt-8">
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

        <ValueChart address={address ?? "0x"} total={total} loading={loading} />

        {(positions.error || balances.error) && <ErrorBox message={(positions.error ?? balances.error)!} />}

        <Holdings
          rows={rows}
          loading={loading}
          usdc={bal("USDC")}
          mon={mon}
          lowGas={mon !== undefined && mon < LOW_GAS}
          baskets={baskets.data}
        />

        <section className="space-y-3">
          <h2 className="px-1 text-lg font-medium">Automation</h2>
          <DelegationCard />
          <ScheduleList />
        </section>

        {address && <FaucetCard address={address} onClaimed={balances.reload} />}

        <section className="space-y-3">
          <h2 className="px-1 text-lg font-medium">Activity</h2>
          <Panel>
            {loading ? (
              <div className="space-y-3 p-5">
                <Skeleton className="h-4 w-2/3" />
                <Skeleton className="h-4 w-1/2" />
              </div>
            ) : activity.length === 0 ? (
              <p className="p-5 text-sm text-muted-foreground">No buys or sells yet.</p>
            ) : (
              <ul className="divide-y divide-border">
                {activity.map((t) => (
                  <li key={`${t.txHash}:${t.logIndex}`} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-5 py-3 text-sm">
                    <span
                      className={cn(
                        "rounded-md border px-2 py-0.5 text-[11px]",
                        t.kind === "mint" ? "border-primary/30 bg-primary/10 text-primary" : "border-border bg-secondary/60 text-muted-foreground",
                      )}
                    >
                      {t.kind === "mint" ? "buy" : "sell"}
                    </span>
                    <Link href={`/baskets/${t.basket}`} className="hover:underline">
                      {nameOf(t.basket)?.symbol ?? shortHash(t.basket)}
                    </Link>
                    <span className="tnum">
                      {usd(BigInt(t.usdcRaw))} · {fmtUnits(t.sharesRaw, SHARE_DECIMALS, 4)} shares
                    </span>
                    <span className="ml-auto text-xs text-muted-foreground">{fmtTime(t.ts)}</span>
                    <a
                      href={txUrl(t.txHash)}
                      target="_blank"
                      rel="noreferrer"
                      className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                    >
                      {shortHash(t.txHash)}
                      <ExternalLink className="size-3" />
                    </a>
                  </li>
                ))}
              </ul>
            )}
          </Panel>
        </section>
      </div>
    </main>
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
