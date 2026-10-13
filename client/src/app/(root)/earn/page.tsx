"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { Plus } from "lucide-react";
import { getMyStrategies, getStrategies, shortHash } from "@/lib/api";
import { useRates } from "@/lib/hooks";
import { useAsync, useSession } from "@/lib/session";
import type { Strategy } from "@/lib/types";
import { cn } from "@/lib/utils";
import { ErrorBox, Skeleton } from "@/components/ui";
import { IconStack } from "@/components/invest/BasketCard";
import { TabPill } from "@/components/motion";
import { pct } from "@/components/earn/Backtest";
import { ASSETS, blendedApy, KIND_LABEL, parts, venueName, protocolOf } from "@/components/earn/sources";
import { ProtocolIcon } from "@/components/ProtocolIcon";
import { FundingStrip } from "@/components/perpl/PerplRisk";

const live = (s: Strategy, rates: ReturnType<typeof useRates>["rates"]) =>
  s.apy?.estimate != null ? Number(s.apy.estimate) : blendedApy(parts(s), rates);

export default function EarnPage() {
  const router = useRouter();
  const { authenticated } = useSession();
  const list = useAsync("strategies", () => getStrategies());
  const { rates } = useRates();
  const [tab, setTab] = useState("all");
  // mine includes private strategies, so it comes from /me
  const mine = useAsync(tab === "mine" && authenticated ? "my-strategies" : null, getMyStrategies);
  const shown = tab === "mine" ? mine : list;
  const rows = (shown.data ?? []).filter((s) => tab === "all" || tab === "mine" || parts(s).some((sl) => sl.asset === tab));
  const best = (f: (s: Strategy) => number | null) =>
    (list.data ?? []).reduce<Strategy | null>((m, s) => ((f(s) ?? -1) > (m ? (f(m) ?? -1) : -1) ? s : m), null);
  const topLive = best((s) => live(s, rates));
  const topBt = best((s) => (s.backtest ? Number(s.backtest.apyUsd) : null));

  return (
    <main className="w-full px-4 pb-24 md:px-6">
      <section className="pb-16 pt-20 text-center md:pb-24 md:pt-28">
        <h1 className="text-[40px] font-medium leading-tight tracking-tight md:text-[60px]">Earn on Monad</h1>
        <p className="mx-auto mt-6 max-w-lg text-pretty leading-7 text-muted-foreground">
          Follow strategies that split one asset across lending, staking, liquidity and funding basis, run from your own wallet.
        </p>
        <Link
          href="/earn/new"
          className="mt-10 inline-flex items-center gap-2 rounded-full bg-primary px-6 py-3 text-sm font-semibold text-primary-foreground transition-colors hover:bg-primary/90"
        >
          <Plus className="size-4" /> Build a strategy
        </Link>
      </section>

      {shown.error && (
        <div className="mx-auto mb-4 max-w-7xl">
          <ErrorBox message={`Strategies unavailable: ${shown.error}`} />
        </div>
      )}

      <section className="mx-auto grid max-w-7xl gap-4 pb-14 md:grid-cols-2">
        {[
          { title: "Highest live APY", s: topLive, v: (s: Strategy) => pct(live(s, rates)) },
          { title: "Best backtest (1y, mainnet data)", s: topBt, v: (s: Strategy) => pct(s.backtest?.apyUsd) },
        ].map(({ title, s, v }) => (
          <article key={title} className="rounded-[22px] bg-secondary/40">
            <p className="px-6 pt-2.5 text-[15px] text-muted-foreground">{title}</p>
            <div className="mt-3 rounded-[22px] border border-white/10 bg-card p-6">
              {s ? (
                <Link href={`/earn/${s.id}`} className="flex items-center justify-between gap-3">
                  <span className="flex min-w-0 items-center gap-2.5">
                    <IconStack symbols={parts(s).map((sl) => sl.asset)} size={26} ring="ring-card" />
                    <span className="truncate text-[22px] leading-none">{s.name}</span>
                  </span>
                  <span className="tnum text-[22px] font-medium text-positive">{v(s)}</span>
                </Link>
              ) : list.loading ? (
                <Skeleton className="h-7 w-full" />
              ) : (
                <p className="text-sm text-muted-foreground">No strategies yet.</p>
              )}
            </div>
          </article>
        ))}
      </section>

      <section className="mx-auto max-w-7xl pb-14">
        <FundingStrip />
      </section>

      <section className="mx-auto max-w-7xl space-y-4">
        <div role="tablist" className="flex flex-wrap items-center gap-1">
          {["all", ...(authenticated ? ["mine"] : []), ...ASSETS].map((t) => (
            <button
              key={t}
              role="tab"
              aria-selected={tab === t}
              onClick={() => setTab(t)}
              className={cn("relative rounded-full px-3 py-1 text-[22px] transition-colors", tab === t ? "text-foreground" : "text-muted-foreground hover:text-foreground")}
            >
              {tab === t && <TabPill id="earn-tab" className="rounded-full bg-secondary" />}
              <span className="relative">{t === "all" ? "All" : t === "mine" ? "Mine" : t}</span>
            </button>
          ))}
        </div>
        <div className="overflow-hidden rounded-3xl border border-white/10 bg-card shadow-[0_8px_40px_rgba(0,0,0,0.25)]">
          <div className="overflow-x-auto">
            <table className="w-full min-w-[760px] border-collapse text-sm">
              <thead>
                <tr className="border-b border-border bg-secondary/40 text-[13px] text-muted-foreground [&>th]:px-4 [&>th]:py-3 [&>th]:font-medium">
                  <th className="pl-6 text-left">Strategy</th>
                  <th className="text-left">Sources</th>
                  <th className="text-right">Live APY</th>
                  <th className="text-right">Backtest APY</th>
                  <th className="pr-6 text-right">Max DD</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {shown.loading && !shown.data ? (
                  [0, 1, 2].map((i) => (
                    <tr key={i} className="h-16">
                      <td colSpan={5} className="px-6">
                        <Skeleton className="h-8 w-full" />
                      </td>
                    </tr>
                  ))
                ) : rows.length === 0 ? (
                  <tr>
                    <td colSpan={5} className="py-10 text-center text-muted-foreground">
                      No strategies here yet.{" "}
                      <Link href="/earn/new" className="text-foreground underline underline-offset-4">
                        Build one
                      </Link>
                      .
                    </td>
                  </tr>
                ) : (
                  rows.map((s) => (
                    <tr
                      key={s.id}
                      tabIndex={0}
                      onClick={() => router.push(`/earn/${s.id}`)}
                      onKeyDown={(e) => e.key === "Enter" && router.push(`/earn/${s.id}`)}
                      className="h-16 cursor-pointer transition-colors hover:bg-secondary/40 [&>td]:px-4"
                    >
                      <td className="pl-6">
                        <div className="flex items-center gap-3">
                          <IconStack symbols={parts(s).map((sl) => sl.asset)} size={26} ring="ring-card" />
                          <div className="min-w-0">
                            <div className="truncate text-[15px] font-medium">{s.name}</div>
                            <div className="text-xs text-muted-foreground">by {shortHash(s.creatorWallet ?? s.creator)}</div>
                          </div>
                        </div>
                      </td>
                      <td>
                        <div className="flex flex-wrap gap-1.5">
                          {parts(s).flatMap((sl, _, all) =>
                            sl.sources.map((src) => (
                              <span key={sl.asset + src.kind + src.venue} className="inline-flex items-center gap-1 rounded-full bg-secondary py-0.5 pl-1 pr-2 text-[11px]">
                                <ProtocolIcon protocol={protocolOf(src.venue)} size={14} />
                                {all.length > 1 && `${sl.asset} `}
                                {KIND_LABEL[src.kind]} · {venueName(src.venue)} {+((sl.weightBps * src.weightBps) / 1e6).toFixed(2)}%
                              </span>
                            )),
                          )}
                        </div>
                      </td>
                      <td className="tnum text-right text-positive">{pct(live(s, rates))}</td>
                      <td className="tnum text-right">{pct(s.backtest?.apyUsd)}</td>
                      <td className="tnum pr-6 text-right text-muted-foreground">{pct(s.backtest?.maxDrawdownUsd)}</td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </div>
      </section>
    </main>
  );
}
