"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useState } from "react";
import { Check, Copy, ExternalLink } from "lucide-react";
import { formatUnits, zeroAddress, type Address } from "viem";
import { fmtTime, fmtUsd, fmtUsdCompact, getBasket, getHistory, getLiveNav, getNavHistory, shortHash } from "@/lib/api";
import { addressUrl, SHARE_DECIMALS, txUrl, USDC, USDC_DECIMALS } from "@/lib/config";
import { useAsync, useSession } from "@/lib/session";
import { fmtUnits } from "@/lib/tokens";
import { readBalance } from "@/lib/tx";
import { useTopic } from "@/lib/ws";
import type { LiveNav } from "@/lib/types";
import { Button, ErrorBox, Panel, Skeleton } from "@/components/ui";
import { TokenIcon } from "@/components/TokenIcon";
import { IconStack, LiquidityBadge } from "@/components/invest/BasketCard";
import { symbolOf, useBasketExtras, useMarkets } from "@/components/invest/extras";
import { NavChart } from "@/components/basket/NavChart";
import { Composition } from "@/components/basket/Composition";
import { TradePanel } from "@/components/basket/TradePanel";
import { ScheduleForm } from "@/components/automation/ScheduleForm";
import { ScheduleList } from "@/components/automation/ScheduleList";
import { cn } from "@/lib/utils";

export default function BasketPage() {
  const address = useParams<{ address: string }>().address.toLowerCase() as Address;
  const { authenticated, address: wallet } = useSession();
  const [tab, setTab] = useState<"buy" | "automate">("buy");
  const [schedulesKey, setSchedulesKey] = useState(0);

  const basket = useAsync(`basket:${address}`, () => getBasket(address));
  const markets = useMarkets();
  const navSnap = useAsync(`nav:${address}`, () => getLiveNav(address));
  const nav = useTopic<LiveNav>(`nav.${address}`) ?? navSnap.data;
  const day = useAsync(`nav-24h:${address}`, () => getNavHistory(address));
  const extras = useBasketExtras(basket.data ? [basket.data] : null)?.get(address);
  const balances = useAsync(wallet ? `balances:${address}:${wallet}` : null, async () => {
    const [usdc, shares, mon] = await Promise.all([
      readBalance(USDC, wallet!),
      readBalance(address, wallet!),
      readBalance(zeroAddress, wallet!),
    ]);
    return { usdc, shares, mon };
  });
  const history = useAsync(authenticated ? `history:${address}` : null, getHistory);

  if (basket.error === "not_found")
    return (
      <main className="w-full px-4 pb-24 pt-8">
        <Panel className="mx-auto max-w-md p-8 text-center">
          <p className="text-lg font-medium">Basket not found</p>
          <p className="mt-2 text-sm text-muted-foreground">No basket lives at {shortHash(address)}.</p>
          <Link href="/" className="mt-5 inline-block">
            <Button variant="ghost">All baskets</Button>
          </Link>
        </Panel>
      </main>
    );

  const b = basket.data;
  const symbols = b?.components.map((c) => symbolOf(c.market, markets.data)) ?? [];
  const navPerShare = nav ? Number(nav.navPerShare) : b?.nav ? Number(b.nav.navPerShare) : null;
  const shares = balances.data?.shares;
  const positionUsd = shares !== undefined && navPerShare !== null ? Number(formatUnits(shares, SHARE_DECIMALS)) * navPerShare : null;
  const dayNavs = (day.data ?? []).map((p) => Number(p.navPerShare));
  const mine = (history.data ?? []).filter((t) => t.basket === address).slice(0, 8);
  const symbol = b?.symbol ?? "shares";
  const refresh = () => {
    balances.reload();
    basket.reload();
    history.reload();
  };

  return (
    <main className="w-full px-4 pb-24 pt-10 md:px-6">
      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_440px]">
        <div className="min-w-0 space-y-4">
          <div className="flex items-center justify-between gap-4">
            <div className="flex min-w-0 items-center gap-3">
              {b ? <IconStack symbols={symbols} size={36} ring="ring-background" /> : <Skeleton className="size-9 rounded-full" />}
              {b ? (
                <h1 className="truncate text-3xl font-medium tracking-tight">
                  {b.name} <span className="text-muted-foreground">{b.symbol}</span>
                </h1>
              ) : (
                <Skeleton className="h-9 w-64" />
              )}
            </div>
            <div className="flex shrink-0 items-center gap-2">
              {nav?.stale && (
                <span className="rounded-full bg-warning/10 px-2 py-0.5 text-[11px] font-medium text-warning">Stale price</span>
              )}
              <LiquidityBadge liquid={extras?.liquid} />
            </div>
          </div>

          {basket.error && <ErrorBox message={basket.error} />}

          <NavChart basket={address} price={navPerShare} />

          <Card title="About">
            <p className="text-[15px] leading-6 text-muted-foreground">
              {b
                ? `${b.name} is an onchain basket holding ${symbols.join(", ")} at fixed weights. Buying with USDC fills every component on its Kuru order book in a single transaction; selling unwinds each slice back to USDC. Shares track the basket's net asset value.`
                : "…"}
            </p>
            <dl className="mt-4 grid gap-x-10 md:grid-cols-2">
              <Detail label="Components">
                <span className="flex flex-wrap justify-end gap-1.5">
                  {b?.components.map((c, i) => (
                    <span key={c.market} className="tnum inline-flex items-center gap-1 rounded-full bg-secondary py-0.5 pl-0.5 pr-2 text-xs font-medium">
                      <TokenIcon symbol={symbols[i]} size={16} />
                      {symbols[i]} {c.weightBps / 100}%
                    </span>
                  ))}
                </span>
              </Detail>
              <Detail label="Onchain address">
                <AddressValue address={address} />
              </Detail>
              <Detail label="Creator">
                {b?.creator ? (
                  <a href={addressUrl(b.creator)} target="_blank" rel="noreferrer" className="tnum hover:underline">
                    {shortHash(b.creator)}
                  </a>
                ) : (
                  "—"
                )}
              </Detail>
              <Detail label="Created at block">
                {b ? (
                  <a href={txUrl(b.createdTx)} target="_blank" rel="noreferrer" className="tnum hover:underline">
                    {b.createdBlock.toLocaleString("en-US")}
                  </a>
                ) : (
                  "—"
                )}
              </Detail>
              <Detail label="Category">
                <span className="rounded-full bg-primary/10 px-2 py-0.5 text-xs font-medium text-primary">Basket</span>
              </Detail>
              <Detail label="Share token">{b ? `ERC-20 · ${b.symbol}` : "—"}</Detail>
            </dl>
          </Card>

          <Card title="Statistics">
            <dl className="grid grid-cols-2 gap-x-6 gap-y-6 md:grid-cols-4">
              <Stat label="TVL" value={nav ? fmtUsdCompact(Number(nav.totalNav)) : null} />
              <Stat label="Supply" value={nav ? Number(nav.supply).toLocaleString("en-US", { maximumFractionDigits: 2 }) : null} />
              <Stat label="24h high" value={day.data ? (dayNavs.length ? fmtUsd(Math.max(...dayNavs)) : "—") : null} />
              <Stat label="24h low" value={day.data ? (dayNavs.length ? fmtUsd(Math.min(...dayNavs)) : "—") : null} />
              <Stat label="Holders" value={b ? String(b.stats?.holders ?? "—") : null} />
              <Stat label="Buys / sells" value={b ? (b.stats ? `${b.stats.mints} / ${b.stats.redeems}` : "—") : null} />
              <Stat label="USDC in" value={b ? (b.stats ? `$${fmtUnits(b.stats.usdcInRaw, USDC_DECIMALS, 2)}` : "—") : null} />
              <Stat
                label="Your position"
                value={!authenticated ? "—" : positionUsd !== null ? fmtUsd(positionUsd) : null}
                note={shares !== undefined ? `${fmtUnits(shares, SHARE_DECIMALS)} ${symbol}` : undefined}
              />
            </dl>
          </Card>

          {b ? <Composition components={b.components} nav={nav} markets={markets.data} /> : <Skeleton className="h-48 w-full rounded-3xl" />}

          {mine.length > 0 && (
            <Card title="Your activity">
              <ul className="-mx-6 divide-y divide-border border-t border-border text-sm">
                {mine.map((t) => (
                  <li key={`${t.txHash}:${t.logIndex}`} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-6 py-3">
                    <span className={t.kind === "mint" ? "text-positive" : "text-destructive"}>
                      {t.kind === "mint" ? "Bought" : "Sold"}
                    </span>
                    <span className="tnum">
                      {fmtUnits(t.sharesRaw, SHARE_DECIMALS)} {symbol} · {fmtUnits(t.usdcRaw, USDC_DECIMALS, 2)} USDC
                    </span>
                    <a
                      href={txUrl(t.txHash)}
                      target="_blank"
                      rel="noreferrer"
                      className="ml-auto inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                    >
                      {fmtTime(t.ts)}
                      <ExternalLink className="size-3" />
                    </a>
                  </li>
                ))}
              </ul>
            </Card>
          )}
        </div>

        <aside className="space-y-3 lg:sticky lg:top-24 lg:self-start">
          <div role="tablist" className="grid grid-cols-2 gap-1 rounded-2xl border border-border bg-secondary/60 p-1">
            {(["buy", "automate"] as const).map((t) => (
              <button
                key={t}
                role="tab"
                aria-selected={tab === t}
                onClick={() => setTab(t)}
                className={cn(
                  "truncate rounded-xl px-4 py-2 text-[15px] font-medium transition-colors",
                  tab === t ? "bg-background text-foreground" : "text-muted-foreground hover:text-foreground",
                )}
              >
                {t === "buy" ? "Buy" : "Automate"} {b?.symbol ?? ""}
              </button>
            ))}
          </div>
          <div className="overflow-hidden rounded-[28px] border border-border bg-secondary/60">
            {tab === "buy" ? (
              <TradePanel basket={address} symbol={symbol} markets={markets.data} balances={balances.data} onTraded={refresh} />
            ) : (
              <div className="space-y-4 p-5">
                <ScheduleForm basket={address} onCreated={() => setSchedulesKey((n) => n + 1)} />
                <ScheduleList key={schedulesKey} basket={address} />
              </div>
            )}
          </div>
        </aside>
      </div>
    </main>
  );
}

function Card({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="rounded-3xl border border-border bg-card p-6">
      <h2 className="mb-3 text-2xl font-medium">{title}</h2>
      {children}
    </section>
  );
}

function Detail({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex min-h-12 items-center justify-between gap-4 border-t border-border py-2 text-sm">
      <dt className="shrink-0 text-muted-foreground">{label}</dt>
      <dd className="min-w-0 text-right">{children}</dd>
    </div>
  );
}

function Stat({ label, value, note }: { label: string; value: React.ReactNode | null; note?: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-sm text-muted-foreground">{label}</dt>
      <dd className="tnum mt-1 truncate text-xl font-medium">{value ?? <Skeleton className="h-7 w-20" />}</dd>
      {note && <dd className="tnum mt-0.5 truncate text-xs text-muted-foreground">{note}</dd>}
    </div>
  );
}

function AddressValue({ address }: { address: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <span className="inline-flex items-center gap-2">
      <span className="tnum">{shortHash(address)}</span>
      <button
        type="button"
        aria-label="Copy address"
        onClick={() =>
          navigator.clipboard.writeText(address).then(
            () => (setCopied(true), setTimeout(() => setCopied(false), 1200)),
            () => {},
          )
        }
        className="text-muted-foreground transition-colors hover:text-foreground"
      >
        {copied ? <Check className="size-4 text-positive" /> : <Copy className="size-4" />}
      </button>
      <a href={addressUrl(address)} target="_blank" rel="noreferrer" aria-label="View on explorer" className="text-muted-foreground hover:text-foreground">
        <ExternalLink className="size-4" />
      </a>
    </span>
  );
}
