"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useState } from "react";
import { ArrowLeft, ExternalLink } from "lucide-react";
import { formatUnits, zeroAddress, type Address } from "viem";
import { fmtTime, fmtUsd, getBasket, getHistory, getLiveNav, getMarkets, shortHash } from "@/lib/api";
import { addressUrl, SHARE_DECIMALS, txUrl, USDC, USDC_DECIMALS } from "@/lib/config";
import { useAsync, useSession } from "@/lib/session";
import { baseToken, fmtUnits } from "@/lib/tokens";
import { readBalance } from "@/lib/tx";
import { useTopic } from "@/lib/ws";
import type { LiveNav } from "@/lib/types";
import { Button, ErrorBox, Label, Panel, Skeleton } from "@/components/ui";
import { Reveal } from "@/components/motion";
import { TokenIcon } from "@/components/TokenIcon";
import { NavChart } from "@/components/basket/NavChart";
import { Composition } from "@/components/basket/Composition";
import { TradePanel } from "@/components/basket/TradePanel";
import { ScheduleForm } from "@/components/automation/ScheduleForm";
import { ScheduleList } from "@/components/automation/ScheduleList";

export default function BasketPage() {
  const address = useParams<{ address: string }>().address.toLowerCase() as Address;
  const { authenticated, address: wallet } = useSession();
  const [schedulesKey, setSchedulesKey] = useState(0);

  const basket = useAsync(`basket:${address}`, () => getBasket(address));
  const markets = useAsync("markets", getMarkets);
  const navSnap = useAsync(`nav:${address}`, () => getLiveNav(address));
  const nav = useTopic<LiveNav>(`nav.${address}`) ?? navSnap.data;
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
          <Link href="/invest" className="mt-5 inline-block">
            <Button variant="ghost">All baskets</Button>
          </Link>
        </Panel>
      </main>
    );

  const b = basket.data;
  const navPerShare = nav ? Number(nav.navPerShare) : b?.nav ? Number(b.nav.navPerShare) : null;
  const shares = balances.data?.shares;
  const positionUsd = shares !== undefined && navPerShare !== null ? Number(formatUnits(shares, SHARE_DECIMALS)) * navPerShare : null;
  const mine = (history.data ?? []).filter((t) => t.basket === address).slice(0, 8);
  const refresh = () => {
    balances.reload();
    basket.reload();
    history.reload();
  };

  return (
    <main className="w-full px-4 pb-24 pt-8">
      <Reveal className="mx-auto w-full max-w-6xl space-y-6">
        <Link
          href="/invest"
          className="inline-flex items-center gap-2 text-sm text-muted-foreground transition-colors hover:text-foreground"
        >
          <ArrowLeft className="size-4" />
          All baskets
        </Link>

        <div className="flex flex-wrap items-end justify-between gap-4">
          <div className="min-w-0 space-y-2">
            <div className="flex items-center gap-3">
              {b ? (
                <h1 className="truncate text-2xl font-medium tracking-tight">{b.name}</h1>
              ) : (
                <Skeleton className="h-8 w-52" />
              )}
              {b && <span className="rounded-md bg-secondary px-2 py-0.5 text-xs text-muted-foreground">{b.symbol}</span>}
            </div>
            <div className="flex flex-wrap items-center gap-2">
              {b?.components.map((c) => {
                const sym = baseToken(c.market, markets.data)?.symbol ?? "…";
                return (
                  <span key={c.market} className="inline-flex items-center gap-1.5 rounded-full bg-secondary/70 py-1 pl-1 pr-3 text-xs">
                    <TokenIcon symbol={sym} size={18} />
                    {sym} <span className="tnum text-muted-foreground">{c.weightBps / 100}%</span>
                  </span>
                );
              })}
              {b?.creator && (
                <a
                  href={addressUrl(b.creator)}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                >
                  by {shortHash(b.creator)}
                  <ExternalLink className="size-3" />
                </a>
              )}
            </div>
          </div>
          <div className="text-right">
            <div className="flex items-center justify-end gap-2 text-sm text-muted-foreground">
              NAV / share
              {nav?.stale && (
                <span className="rounded-md border border-warning/30 bg-warning/10 px-2 py-0.5 text-[11px] text-warning">stale</span>
              )}
            </div>
            <div className="tnum text-3xl font-medium tracking-tight">
              {navPerShare !== null ? fmtUsd(navPerShare) : <Skeleton className="ml-auto h-9 w-32" />}
            </div>
          </div>
        </div>

        {basket.error && <ErrorBox message={basket.error} />}

        <div className="grid gap-4 sm:grid-cols-3">
          <Stat label="TVL" value={nav ? fmtUsd(Number(nav.totalNav)) : null} />
          <Stat
            label="Supply"
            value={nav ? `${Number(nav.supply).toLocaleString("en-US", { maximumFractionDigits: 4 })} ${b?.symbol ?? ""}` : null}
          />
          <Stat
            label="Your position"
            value={!authenticated ? "—" : positionUsd !== null ? fmtUsd(positionUsd) : null}
            note={shares !== undefined ? `${fmtUnits(shares, SHARE_DECIMALS)} ${b?.symbol ?? ""}` : undefined}
          />
        </div>

        <div className="grid gap-6 lg:grid-cols-3">
          <div className="min-w-0 space-y-6 lg:col-span-2">
            <NavChart basket={address} />
            {b ? (
              <Composition components={b.components} nav={nav} markets={markets.data} />
            ) : (
              <Skeleton className="h-48 w-full rounded-2xl" />
            )}

            <Panel>
              <div className="px-5 py-5">
                <Label>Activity</Label>
              </div>
              {b?.stats && (
                <dl className="grid grid-cols-2 gap-4 border-t border-border px-5 py-4 text-sm sm:grid-cols-4">
                  <Mini label="Holders" value={String(b.stats.holders)} />
                  <Mini label="Buys / sells" value={`${b.stats.mints} / ${b.stats.redeems}`} />
                  <Mini label="USDC in" value={fmtUnits(b.stats.usdcInRaw, USDC_DECIMALS, 2)} />
                  <Mini label="USDC out" value={fmtUnits(b.stats.usdcOutRaw, USDC_DECIMALS, 2)} />
                </dl>
              )}
              {mine.length > 0 && (
                <ul className="divide-y divide-border border-t border-border text-sm">
                  {mine.map((t) => (
                    <li key={`${t.txHash}:${t.logIndex}`} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-5 py-3">
                      <span className={t.kind === "mint" ? "text-positive" : "text-destructive"}>
                        {t.kind === "mint" ? "You bought" : "You sold"}
                      </span>
                      <span className="tnum">
                        {fmtUnits(t.sharesRaw, SHARE_DECIMALS)} {b?.symbol} · {fmtUnits(t.usdcRaw, USDC_DECIMALS, 2)} USDC
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
              )}
            </Panel>

            <section className="space-y-4">
              <Label>Automation</Label>
              <ScheduleForm basket={address} onCreated={() => setSchedulesKey((n) => n + 1)} />
              <ScheduleList key={schedulesKey} basket={address} />
            </section>
          </div>

          <div className="lg:sticky lg:top-24 lg:self-start">
            <TradePanel
              basket={address}
              symbol={b?.symbol ?? "shares"}
              markets={markets.data}
              balances={balances.data}
              onTraded={refresh}
            />
          </div>
        </div>
      </Reveal>
    </main>
  );
}

function Stat({ label, value, note }: { label: string; value: React.ReactNode | null; note?: string }) {
  return (
    <Panel className="p-5">
      <div className="text-sm text-muted-foreground">{label}</div>
      <div className="tnum mt-1 truncate text-2xl font-medium tracking-tight">
        {value ?? <Skeleton className="h-8 w-24" />}
      </div>
      {note && <div className="tnum mt-1 truncate text-xs text-muted-foreground">{note}</div>}
    </Panel>
  );
}

function Mini({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="tnum mt-0.5">{value}</dd>
    </div>
  );
}
