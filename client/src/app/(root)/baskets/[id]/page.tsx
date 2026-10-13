"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useState } from "react";
import { motion } from "motion/react";
import { ApiError, fmtTime, fmtUsd, getBasket, getBook, getCandles, getMyBaskets, runBacktest, shortHash, type BacktestBody } from "@/lib/api";
import { mid, useHoldings } from "@/lib/hooks";
import { useAsync, useSession } from "@/lib/session";
import { marketOf } from "@/lib/tokens";
import type { Basket } from "@/lib/types";
import { Button, Card, Detail, ErrorBox, Panel, PanelTabs, Skeleton, Stat } from "@/components/ui";
import { TokenIcon } from "@/components/TokenIcon";
import { IconStack } from "@/components/invest/BasketCard";
import { PriceChart, RANGES, type Range } from "@/components/chart/PriceChart";
import { basketIndex } from "@/components/basket/index";
import { BasketTrade } from "@/components/basket/BasketTrade";
import { ScheduleForm } from "@/components/automation/ScheduleForm";
import { ScheduleList } from "@/components/automation/ScheduleList";
import { BacktestView } from "@/components/earn/Backtest";
import { AutoHeight } from "@/components/motion";

const BT_USD = 1000;
const baseOf = (market: string) => marketOf(market)?.token.symbol ?? market;

// buy-and-hold of the leg tokens at their weights (history "hold" legs); multi-token runs take 2..5 sleeves
function basketBacktest(legs: Basket["legs"], px: Record<string, number | null>): BacktestBody | null {
  const hold = [{ kind: "hold", weight: "1" }];
  if (legs.length === 1) {
    const p = px[legs[0].market];
    return p ? { asset: baseOf(legs[0].market), amount: String(BT_USD / p), legs: hold } : null;
  }
  if (legs.length > 5) return null;
  return { amount: String(BT_USD), sleeves: legs.map((l) => ({ asset: baseOf(l.market), amount: "1", legs: hold, weight: String(l.weightBps / 10000) })) };
}

// public first; private baskets resolve through /me/baskets for their owner
async function loadBasket(id: string, authed: boolean): Promise<Basket> {
  try {
    return await getBasket(id);
  } catch (e) {
    if (!(authed && e instanceof ApiError && e.status === 404)) throw e;
    const own = (await getMyBaskets()).find((b) => b.id === id);
    if (!own) throw e;
    return own;
  }
}

export default function BasketPage() {
  const id = useParams<{ id: string }>().id;
  const { ready, authenticated } = useSession();
  const [range, setRange] = useState<Range>("1W");
  const [tab, setTab] = useState<"trade" | "auto">("trade");
  const [key, setKey] = useState(0);
  const basket = useAsync(ready ? `basket:${id}:${authenticated}` : null, () => loadBasket(id, authenticated));
  const b = basket.data;
  const legs = b?.legs ?? [];
  const legKey = legs.map((l) => l.market).join();
  const { iv, ms } = RANGES[range];
  const series = useAsync(b ? `bidx:${legKey}:${range}` : null, async () =>
    basketIndex(
      await Promise.all(legs.map(async (l) => ({ weightBps: l.weightBps, candles: await getCandles(marketOf(l.market)!.orderbook, iv, ms === null ? undefined : Date.now() - ms) }))),
    ),
  );
  const prices = useAsync(b ? `bpx:${legKey}` : null, async () => {
    const books = await Promise.allSettled(legs.map((l) => getBook(marketOf(l.market)!.orderbook)));
    return Object.fromEntries(legs.map((l, i) => [l.market, books[i].status === "fulfilled" ? mid(books[i].value) : null]));
  });
  const holdings = useHoldings();
  const btBody = b && prices.data ? basketBacktest(legs, prices.data) : null;
  const bt = useAsync(btBody ? `bbt:${legKey}` : null, () => runBacktest(btBody!));

  if (basket.error === "not_found")
    return (
      <main className="w-full px-4 pb-24 pt-8 md:px-6">
        <Panel className="mx-auto max-w-md p-8 text-center">
          <p className="text-lg font-medium">Basket not found</p>
          <Link href="/?tab=baskets" className="mt-5 inline-block">
            <Button variant="ghost">All baskets</Button>
          </Link>
        </Panel>
      </main>
    );

  const held = holdings.data?.baskets.find((h) => h.basketId === id);
  const heldUsd = held && prices.data ? held.legs.reduce((s, l) => s + Number(l.amount) * (prices.data![l.market] ?? 0), 0) : null;
  const symbols = legs.map((l) => baseOf(l.market));
  const pts = series.data ?? [];

  return (
    <main className="w-full px-4 pb-24 pt-10 md:px-6">
      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_440px]">
        <div className="min-w-0 space-y-4">
          <div className="flex items-center justify-between gap-4">
            <div className="flex min-w-0 items-center gap-3">
              {b ? <IconStack symbols={symbols} size={36} ring="ring-background" /> : <Skeleton className="size-9 rounded-full" />}
              {b ? <h1 className="truncate text-3xl font-medium tracking-tight">{b.name}</h1> : <Skeleton className="h-9 w-64" />}
            </div>
            {b && (
              <span className="rounded-full bg-primary/10 px-2 py-0.5 text-[11px] font-medium text-primary">{b.public ? "Public basket" : "Private"}</span>
            )}
          </div>
          {basket.error && <ErrorBox message={basket.error} />}

          <PriceChart
            points={pts}
            price={pts.at(-1)?.v ?? null}
            range={range}
            onRange={setRange}
            loading={!b || series.loading}
            error={series.error}
            caption="Value of $100, weighted"
            dataKey={`${id}:${range}`}
            empty="No candle history for every leg in this range yet."
          />

          <Card title="Legs">
            <table className="w-full text-sm">
              <thead className="text-left text-xs text-muted-foreground">
                <tr className="[&>th]:py-2 [&>th]:font-normal">
                  <th>Token</th>
                  <th className="text-right">Weight</th>
                  <th className="text-right">Price</th>
                  <th className="text-right">You hold</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border border-t border-border">
                {legs.map((l) => {
                  const h = held?.legs.find((x) => x.market === l.market);
                  return (
                    <tr key={l.market} className="[&>td]:py-3">
                      <td>
                        <Link href={`/trade/${l.market}`} className="inline-flex items-center gap-2 font-medium hover:underline">
                          <TokenIcon symbol={baseOf(l.market)} size={20} />
                          {baseOf(l.market)}
                        </Link>
                      </td>
                      <td className="tnum text-right">{l.weightBps / 100}%</td>
                      <td className="tnum text-right">{prices.data?.[l.market] ? fmtUsd(prices.data[l.market]!) : prices.loading ? "…" : "-"}</td>
                      <td className="tnum text-right">{h ? Number(h.amount).toLocaleString("en-US", { maximumFractionDigits: 6 }) : "-"}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </Card>

          <Card title="Statistics">
            <dl className="grid grid-cols-2 gap-6 md:grid-cols-4">
              <Stat label="Legs" value={b ? String(legs.length) : null} />
              <Stat label={`${range} return`} value={pts.length > 1 ? `${(pts.at(-1)!.v - 100).toFixed(2)}%` : series.loading ? null : "-"} />
              <Stat label="Your holding" value={!authenticated ? "-" : heldUsd !== null ? fmtUsd(heldUsd) : held === undefined && holdings.data ? "$0" : null} />
              <Stat label="Created" value={b ? new Date(b.createdAt).toLocaleDateString() : null} />
            </dl>
            <dl className="mt-4">
              <Detail label="Owner">{b ? shortHash(b.owner) : "-"}</Detail>
              <Detail label="Created at">{b ? fmtTime(b.createdAt) : "-"}</Detail>
            </dl>
          </Card>

          <Card title="Backtest">
            <p className="-mt-1 mb-4 text-xs text-muted-foreground">
              {fmtUsd(BT_USD)} split by weight and held, on mainnet history. No rebalancing.
            </p>
            {legs.length > 5 ? (
              <p className="text-sm text-muted-foreground">Backtests support up to 5 tokens.</p>
            ) : (
              <BacktestView bt={bt.data ?? null} loading={!btBody || bt.loading} error={bt.error} />
            )}
          </Card>
        </div>

        <aside className="space-y-3 lg:sticky lg:top-24 lg:self-start">
          <PanelTabs
            id="basket"
            value={tab}
            onChange={setTab}
            options={[
              { value: "trade", label: "Market" },
              { value: "auto", label: "Automate" },
            ]}
          />
          <AutoHeight className="rounded-[28px] border border-border bg-secondary/60">
            <motion.div key={tab} initial={{ opacity: 0 }} animate={{ opacity: 1 }} transition={{ duration: 0.2 }}>
              {!b ? (
                <div className="p-5">
                  <Skeleton className="h-40 w-full" />
                </div>
              ) : tab === "trade" ? (
                <BasketTrade basket={b} holdings={holdings.data} heldUsd={heldUsd} onDone={() => (holdings.reload(), prices.reload())} />
              ) : (
                <div className="space-y-4 p-5">
                  <ScheduleForm target={{ type: "basket", id }} current={heldUsd} onCreated={() => setKey((n) => n + 1)} />
                  <ScheduleList key={key} target={{ type: "basket", id }} />
                </div>
              )}
            </motion.div>
          </AutoHeight>
        </aside>
      </div>
    </main>
  );
}
