"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { fmtTime, getStrategy, runBacktest, shortHash } from "@/lib/api";
import { useRates } from "@/lib/hooks";
import { useAsync, useSession } from "@/lib/session";
import { addressUrl } from "@/lib/config";
import { Button, Card, Detail, ErrorBox, Panel, Skeleton, Stat } from "@/components/ui";
import { TokenIcon } from "@/components/TokenIcon";
import { AllocationDonut, BacktestView, pct } from "@/components/earn/Backtest";
import { DeployPanel } from "@/components/earn/DeployPanel";
import { blendedApy, KIND_LABEL, leverageOf, sourceApy, toBacktest, venueName } from "@/components/earn/sources";

export default function StrategyPage() {
  const id = useParams<{ id: string }>().id;
  const { ready, authenticated } = useSession();
  const st = useAsync(ready ? `strategy:${id}:${authenticated}` : null, () => getStrategy(id));
  const s = st.data;
  const { rates } = useRates();
  // ponytail: backtest straight from history (no api write per view); api's stored summary shown when history is down
  const body = s ? toBacktest(s.asset, s.sources, s.rules, s.asset === "USDC" ? "1000" : "1") : null;
  const bt = useAsync(s ? `bt:${s.id}` : null, () => runBacktest(body!));

  if (st.error === "not_found")
    return (
      <main className="w-full px-4 pb-24 pt-8 md:px-6">
        <Panel className="mx-auto max-w-md p-8 text-center">
          <p className="text-lg font-medium">Strategy not found</p>
          <Link href="/earn" className="mt-5 inline-block">
            <Button variant="ghost">All strategies</Button>
          </Link>
        </Panel>
      </main>
    );

  const live = s ? (s.apy?.estimate != null ? Number(s.apy.estimate) : blendedApy(s.sources, s.asset, rates)) : null;

  return (
    <main className="w-full px-4 pb-24 pt-10 md:px-6">
      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_440px]">
        <div className="min-w-0 space-y-4">
          <div className="flex items-center justify-between gap-4">
            <div className="flex min-w-0 items-center gap-3">
              {s ? <TokenIcon symbol={s.asset} size={36} /> : <Skeleton className="size-9 rounded-full" />}
              {s ? (
                <h1 className="truncate text-3xl font-medium tracking-tight">
                  {s.name} <span className="text-muted-foreground">{s.asset}</span>
                </h1>
              ) : (
                <Skeleton className="h-9 w-64" />
              )}
            </div>
            {s && !s.public && <span className="rounded-full bg-secondary px-2 py-0.5 text-[11px] text-muted-foreground">Private</span>}
          </div>
          {st.error && <ErrorBox message={st.error} />}

          <section className="rounded-[32px] bg-primary/[0.06] p-6">
            <dl className="grid grid-cols-2 gap-6 md:grid-cols-4">
              <Stat label="Live APY (est.)" value={s ? <span className="text-positive">{pct(live)}</span> : null} note={s?.apy?.stale ? "rates stale" : "Σ weight × live rate"} />
              <Stat label="Backtest APY" value={s ? pct(bt.data?.apyUsd ?? s.backtest?.apyUsd) : null} note="historical, mainnet data" />
              <Stat label="Max drawdown" value={s ? pct(bt.data?.maxDrawdownUsd ?? s.backtest?.maxDrawdownUsd) : null} />
              <Stat label="HODL APY" value={s ? pct(bt.data?.hodl.apyUsd ?? s.backtest?.hodlApyUsd) : null} />
            </dl>
          </section>

          <Card title="Allocation">{s ? <AllocationDonut sources={s.sources} /> : <Skeleton className="h-40 w-full" />}</Card>

          <Card title="Sources">
            <ul className="divide-y divide-border border-t border-border text-sm">
              {s?.sources.map((src) => {
                const apy = sourceApy(src, s.asset, rates);
                return (
                  <li key={`${src.kind}${src.venue}`} className="flex flex-wrap items-center gap-x-3 gap-y-1 py-3">
                    <span className="rounded-md bg-secondary/60 px-2 py-0.5 text-[11px] text-muted-foreground">{KIND_LABEL[src.kind]}</span>
                    <span className="font-medium">{venueName(src.venue)}</span>
                    {src.kind === "basis" && <span className="text-xs text-muted-foreground">{leverageOf(src)}× short</span>}
                    {src.kind === "lp" && <span className="text-xs text-warning">estimated · 4-day lock</span>}
                    <span className="tnum ml-auto text-muted-foreground">{src.weightBps / 100}%</span>
                    <span className="tnum w-20 text-right text-positive">{apy === null ? "—" : pct(apy)}</span>
                  </li>
                );
              })}
            </ul>
          </Card>

          <Card title="Backtest">
            <BacktestView bt={bt.data} loading={bt.loading} error={bt.error} />
          </Card>

          <Card title="Rules">
            {s ? (
              <dl className="grid gap-x-10 md:grid-cols-2">
                <Detail label="Drift band">{(s.rules.driftBps ?? 500) / 100}%</Detail>
                <Detail label="Min rebalance">{((s.rules.minRebalanceSeconds ?? 86400) / 3600).toFixed(0)}h</Detail>
                <Detail label="Funding gate">
                  {s.rules.fundingGate ? `short APR ≥ ${pct(s.rules.fundingGate.apr, 1)} over ${s.rules.fundingGate.lookbackDays}d` : "off"}
                </Detail>
                <Detail label="Creator">
                  {s.creatorWallet ? (
                    <a href={addressUrl(s.creatorWallet)} target="_blank" rel="noreferrer" className="tnum hover:underline">
                      {shortHash(s.creatorWallet)}
                    </a>
                  ) : (
                    shortHash(s.creator)
                  )}
                </Detail>
                <Detail label="Created">{fmtTime(s.createdAt)}</Detail>
                <Detail label="Fees">0.4% on open · 0 on withdraw</Detail>
              </dl>
            ) : (
              <Skeleton className="h-24 w-full" />
            )}
          </Card>
        </div>

        <aside className="space-y-3 lg:sticky lg:top-24 lg:self-start">
          <div className="rounded-[28px] border border-border bg-secondary/60">{s ? <DeployPanel strategy={s} /> : <Skeleton className="m-5 h-60" />}</div>
        </aside>
      </div>
    </main>
  );
}
