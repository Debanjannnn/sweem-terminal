"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useState } from "react";
import { motion } from "motion/react";
import { fmtUsd, fmtUsdCompact } from "@/lib/api";
import { addressUrl } from "@/lib/config";
import { mid, useBook, useCandles, useHoldings } from "@/lib/hooks";
import { marketOf } from "@/lib/tokens";
import { Button, Card, Detail, Panel, PanelTabs, Stat } from "@/components/ui";
import { TokenIcon } from "@/components/TokenIcon";
import { PriceChart, RANGES, type Range } from "@/components/chart/PriceChart";
import { OrderBook } from "@/components/trade/OrderBook";
import { MarketPanel } from "@/components/trade/MarketPanel";
import { LimitPanel } from "@/components/trade/LimitPanel";
import { ScheduleForm } from "@/components/automation/ScheduleForm";
import { ScheduleList } from "@/components/automation/ScheduleList";
import { AutoHeight } from "@/components/motion";

type Tab = "market" | "limit" | "auto";

export default function TradePage() {
  const market = marketOf(decodeURIComponent(useParams<{ symbol: string }>().symbol));
  const [range, setRange] = useState<Range>("1D");
  const [mode, setMode] = useState<"line" | "candle">("line");
  const [tab, setTab] = useState<Tab>("market");
  const [key, setKey] = useState(0);
  const { book, stale, error: bookErr } = useBook(market?.orderbook);
  const { iv, ms } = RANGES[range];
  const chart = useCandles(market?.orderbook, iv, ms);
  const day24 = useCandles(market?.orderbook, "1h", 86400e3);
  const holdings = useHoldings();

  if (!market)
    return (
      <main className="w-full px-4 pb-24 pt-8 md:px-6">
        <Panel className="mx-auto max-w-md p-8 text-center">
          <p className="text-lg font-medium">Market not found</p>
          <Link href="/" className="mt-5 inline-block">
            <Button variant="ghost">All markets</Button>
          </Link>
        </Panel>
      </main>
    );

  const t = market.token;
  const lastClose = Number(day24.candles.at(-1)?.c ?? chart.candles.at(-1)?.c ?? 0);
  const price = mid(book) ?? (lastClose || null);
  const points = chart.candles.map((c) => ({ t: c.t, v: Number(c.c), o: Number(c.o), h: Number(c.h), l: Number(c.l) }));
  const hs = day24.candles.map((c) => Number(c.h));
  const ls = day24.candles.map((c) => Number(c.l));
  const vol = day24.candles.reduce((s, c) => s + Number(c.q), 0);
  const held = holdings.data?.tokens.find((x) => x.symbol === t.symbol);

  return (
    <main className="w-full px-4 pb-24 pt-10 md:px-6">
      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_440px]">
        <div className="min-w-0 space-y-4">
          <div className="flex items-center justify-between gap-4">
            <div className="flex min-w-0 items-center gap-3">
              <TokenIcon symbol={t.symbol} size={36} />
              <h1 className="truncate text-3xl font-medium tracking-tight">
                {t.name} <span className="text-muted-foreground">{t.symbol}</span>
              </h1>
            </div>
            {stale && <span className="rounded-full bg-warning/10 px-2 py-0.5 text-[11px] font-medium text-warning">Stale price</span>}
          </div>

          <PriceChart
            points={points}
            price={price}
            range={range}
            onRange={setRange}
            mode={mode}
            onMode={setMode}
            loading={chart.loading}
            error={chart.error}
            caption={`${t.symbol} / USDC`}
          />

          <Card title="Statistics">
            <dl className="grid grid-cols-2 gap-6 md:grid-cols-4">
              <Stat label="24h high" value={day24.loading ? null : hs.length ? fmtUsd(Math.max(...hs)) : "—"} />
              <Stat label="24h low" value={day24.loading ? null : ls.length ? fmtUsd(Math.min(...ls)) : "—"} />
              <Stat label="24h volume" value={day24.loading ? null : day24.candles.length ? fmtUsdCompact(vol) : "—"} />
              <Stat label="Your balance" value={held ? Number(held.amount).toLocaleString("en-US", { maximumFractionDigits: 6 }) : "—"} note={held && price ? fmtUsd(Number(held.amount) * price) : undefined} />
            </dl>
          </Card>

          <OrderBook book={book} symbol={t.symbol} stale={stale} error={bookErr} />

          <Card title="About">
            <dl className="grid gap-x-10 md:grid-cols-2">
              <Detail label="Token">
                <a href={addressUrl(t.address)} target="_blank" rel="noreferrer" className="tnum hover:underline">
                  {t.address.slice(0, 8)}…{t.address.slice(-6)}
                </a>
              </Detail>
              <Detail label="Kuru order book">
                <a href={addressUrl(market.orderbook)} target="_blank" rel="noreferrer" className="tnum hover:underline">
                  {market.orderbook.slice(0, 8)}…{market.orderbook.slice(-6)}
                </a>
              </Detail>
              <Detail label="Min order">{`${market.minSize} ${t.symbol}`}</Detail>
              <Detail label="Network">Monad mainnet</Detail>
            </dl>
          </Card>
        </div>

        <aside className="space-y-3 lg:sticky lg:top-24 lg:self-start">
          <PanelTabs
            id="trade"
            value={tab}
            onChange={setTab}
            options={[
              { value: "market", label: "Market" },
              { value: "limit", label: "Limit" },
              { value: "auto", label: "Automate" },
            ]}
          />
          <AutoHeight className="rounded-[28px] border border-border bg-secondary/60">
            <motion.div key={tab} initial={{ opacity: 0 }} animate={{ opacity: 1 }} transition={{ duration: 0.2 }}>
              {tab === "market" ? (
                <MarketPanel market={market} holdings={holdings.data} onDone={holdings.reload} />
              ) : tab === "limit" ? (
                <LimitPanel market={market} mid={price} />
              ) : (
                <div className="space-y-4 p-5">
                  <ScheduleForm target={{ type: "market", id: market.symbol }} current={price} onCreated={() => setKey((n) => n + 1)} />
                  <ScheduleList key={key} target={{ type: "market", id: market.symbol }} />
                </div>
              )}
            </motion.div>
          </AutoHeight>
        </aside>
      </div>
    </main>
  );
}
