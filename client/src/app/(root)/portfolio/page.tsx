"use client";

import Link from "next/link";
import { ExternalLink } from "lucide-react";
import { fmtPctSigned, fmtTime, fmtUsd, getBaskets, getHistory, getPositions, shortHash } from "@/lib/api";
import { useAsync, useSession } from "@/lib/session";
import { SHARE_DECIMALS, txUrl, USDC_DECIMALS } from "@/lib/config";
import { fmtUnits } from "@/lib/tokens";
import { Button, ErrorBox, Panel, Skeleton } from "@/components/ui";
import { Reveal } from "@/components/motion";
import { cn } from "@/lib/utils";
import { Balances, FaucetCard, useBalances } from "@/components/portfolio/Wallet";
import { pnlByBasket } from "@/components/portfolio/pnl";
import { DelegationCard } from "@/components/automation/Delegation";
import { ScheduleList } from "@/components/automation/ScheduleList";

const usd = (raw: bigint) => `${raw < BigInt(0) ? "-" : ""}$${fmtUnits(raw < BigInt(0) ? -raw : raw, USDC_DECIMALS, 2)}`;
const signedUsd = (raw: bigint) => `${raw > BigInt(0) ? "+" : ""}${usd(raw)}`;
const pnlTone = (raw: bigint) => (raw > BigInt(0) ? "text-positive" : raw < BigInt(0) ? "text-destructive" : "text-muted-foreground");
const pct = (pnl: bigint, base: bigint) => (base > BigInt(0) ? fmtPctSigned((Number(pnl) / Number(base)) * 100) : null);

export default function PortfolioPage() {
  const { ready, authenticated, login, address } = useSession();
  const on = ready && authenticated;
  const positions = useAsync(on ? "positions" : null, getPositions);
  const history = useAsync(on ? "history" : null, getHistory);
  const baskets = useAsync(on ? "baskets" : null, () => getBaskets());
  const balances = useBalances(on ? address : undefined);

  if (ready && !authenticated) {
    return (
      <main className="w-full px-4 pb-24 pt-8">
        <Panel className="mx-auto w-full max-w-3xl">
          <div className="flex items-center justify-between gap-3 p-5">
            <span className="text-sm text-muted-foreground">Connect to see your positions.</span>
            <Button onClick={login}>Connect</Button>
          </div>
        </Panel>
      </main>
    );
  }

  const loading = !ready || positions.loading || history.loading;
  const pnl = pnlByBasket(history.data ?? [], positions.data ?? []);
  const held = (positions.data ?? []).filter((p) => BigInt(p.sharesRaw) > BigInt(0));
  let total = BigInt(0), cost = BigInt(0), gain = BigInt(0), minted = BigInt(0);
  for (const p of pnl.values()) {
    total += p.valueRaw;
    cost += p.costRaw;
    gain += p.pnlRaw;
    minted += p.mintedRaw;
  }
  const nameOf = (addr: string) => baskets.data?.find((b) => b.address === addr);
  const activity = [...(history.data ?? [])].sort((a, b) => b.ts.localeCompare(a.ts)).slice(0, 25);

  return (
    <main className="w-full px-4 pb-24 pt-8">
      <Reveal className="mx-auto w-full max-w-3xl space-y-6">
        <h1 className="text-lg font-medium">Portfolio</h1>

        <div className="grid grid-cols-2 gap-6 px-1 sm:grid-cols-3">
          <Stat label="Total value" loading={loading} value={usd(total)} />
          <Stat label="Net invested" loading={loading} value={usd(cost)} />
          <Stat
            label="PnL"
            loading={loading}
            value={
              <span className={pnlTone(gain)}>
                {signedUsd(gain)}
                {pct(gain, minted) && <span className="ml-2 text-base">{pct(gain, minted)}</span>}
              </span>
            }
          />
        </div>

        {(positions.error || history.error) && <ErrorBox message={(positions.error ?? history.error)!} />}

        <Balances balances={balances} />
        {address && <FaucetCard address={address} onClaimed={balances.reload} />}

        <section className="space-y-3">
          <h2 className="px-1 font-medium">Positions</h2>
          <Panel>
            {loading ? (
              <ul className="divide-y divide-border">
                {[0, 1, 2].map((i) => (
                  <li key={i} className="flex items-center gap-3 p-5">
                    <Skeleton className="h-4 w-24" />
                    <Skeleton className="ml-auto h-4 w-20" />
                    <Skeleton className="h-4 w-16" />
                  </li>
                ))}
              </ul>
            ) : held.length === 0 ? (
              <div className="flex items-center justify-between p-5 text-sm">
                <span className="text-muted-foreground">No positions yet.</span>
                <Link href="/invest" className="underline underline-offset-4 hover:no-underline">
                  Browse baskets
                </Link>
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full min-w-[560px] text-sm">
                  <thead className="text-left text-xs text-muted-foreground">
                    <tr className="border-b border-border">
                      <th className="px-5 py-3 font-normal">Basket</th>
                      <th className="px-3 py-3 text-right font-normal">Shares</th>
                      <th className="px-3 py-3 text-right font-normal">NAV / share</th>
                      <th className="px-3 py-3 text-right font-normal">Value</th>
                      <th className="px-5 py-3 text-right font-normal">PnL</th>
                    </tr>
                  </thead>
                  <tbody className="tnum divide-y divide-border">
                    {held.map((p) => {
                      const b = nameOf(p.basket);
                      const r = pnl.get(p.basket)!;
                      return (
                        <tr key={p.basket} className="transition-colors hover:bg-secondary/30">
                          <td className="px-5 py-4">
                            <Link href={`/baskets/${p.basket}`} className="font-medium hover:underline">
                              {b?.name ?? shortHash(p.basket)}
                            </Link>
                            {b && <span className="ml-2 text-xs text-muted-foreground">{b.symbol}</span>}
                          </td>
                          <td className="px-3 py-4 text-right">{fmtUnits(p.sharesRaw, SHARE_DECIMALS, 4)}</td>
                          <td className="px-3 py-4 text-right">{p.navPerShare ? fmtUsd(Number(p.navPerShare)) : "—"}</td>
                          <td className="px-3 py-4 text-right">{p.valueUsdc ? usd(r.valueRaw) : "—"}</td>
                          <td className={cn("px-5 py-4 text-right", pnlTone(r.pnlRaw))}>
                            {signedUsd(r.pnlRaw)}
                            {pct(r.pnlRaw, r.mintedRaw) && <div className="text-xs">{pct(r.pnlRaw, r.mintedRaw)}</div>}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </Panel>
        </section>

        <section className="space-y-3">
          <h2 className="px-1 font-medium">Activity</h2>
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

        <section className="space-y-3">
          <h2 className="px-1 font-medium">Automation</h2>
          <DelegationCard />
          <ScheduleList />
        </section>
      </Reveal>
    </main>
  );
}

function Stat({ label, value, loading }: { label: string; value: React.ReactNode; loading: boolean }) {
  return (
    <div>
      <div className="text-sm text-muted-foreground">{label}</div>
      <div className="tnum mt-1 text-2xl font-medium tracking-tight sm:text-3xl">
        {loading ? <Skeleton className="h-9 w-28" /> : value}
      </div>
    </div>
  );
}
