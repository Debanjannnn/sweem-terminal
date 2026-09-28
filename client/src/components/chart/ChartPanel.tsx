"use client";

import { useMemo, useState } from "react";
import { Segmented } from "@/components/ui/Segmented";
import { Skeleton } from "@/components/ui/Skeleton";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { useCandles } from "@/lib/query";
import { useTopic } from "@/lib/ws";
import { errorMessage, fromWad, type Candle, type Market } from "@/lib/api";
import { formatSize, formatUsd } from "@/lib/format";
import { DepthPanel } from "./DepthPanel";
import { PriceChart, type Row } from "./PriceChart";

const TABS = [
  { value: "chart", label: "Chart" },
  { value: "depth", label: "Depth" },
  { value: "funding", label: "Funding" },
  { value: "details", label: "Details" },
] as const;

const STYLES = [
  { value: "candle", label: "Candle" },
  { value: "line", label: "Line" },
] as const;

// bar intervals both venues serve: kuru's aggregator and perpl's candles
// overlap on exactly these. the chart pans and zooms through the history.
const INTERVALS = [
  { value: "60", label: "1m" },
  { value: "300", label: "5m" },
  { value: "900", label: "15m" },
  { value: "3600", label: "1H" },
  { value: "14400", label: "4H" },
  { value: "86400", label: "1D" },
] as const;

// perpl answers at most 1024 bars a request
const BARS = 1000;

type Tab = (typeof TABS)[number]["value"];
type Style = (typeof STYLES)[number]["value"];
type Interval = (typeof INTERVALS)[number]["value"];

const toRow = (c: Candle): Row => ({
  t: c.openTime,
  o: fromWad(c.open),
  h: fromWad(c.high),
  l: fromWad(c.low),
  c: fromWad(c.close),
  v: fromWad(c.volume),
});

export function ChartPanel({ market }: { market?: Market | null }) {
  const [tab, setTab] = useState<Tab>("chart");
  const [style, setStyle] = useState<Style>("candle");
  const [interval, setBarInterval] = useState<Interval>("900");

  const resolution = Number(interval);
  // the window only moves when the interval does, so the query key stays stable
  const window = useMemo(() => {
    const to = Math.floor(Date.now() / 1000);
    return { from: to - BARS * resolution, to };
  }, [resolution]);

  const id = market?.id ?? null;
  const candles = useCandles(id, resolution, window.from, window.to);
  const live = useTopic<Candle>(
    id ? `candle.${id.toLowerCase()}.${resolution}` : null,
  );

  const rows = useMemo(() => {
    const out = (candles.data ?? []).map(toRow);
    if (!live.data || live.data.resolution !== resolution) return out;
    const next = toRow(live.data);
    const last = out[out.length - 1];
    if (last && last.t === next.t) out[out.length - 1] = next;
    else if (!last || next.t > last.t) out.push(next);
    return out;
  }, [candles.data, live.data, resolution]);

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 p-4">
      <Segmented
        id="chart-tabs"
        value={tab}
        onChange={setTab}
        options={TABS}
        className="w-fit"
      />

      {tab === "chart" ? (
        <>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <Segmented id="chart-style" value={style} onChange={setStyle} options={STYLES} />
            <Segmented id="chart-interval" value={interval} onChange={setBarInterval} options={INTERVALS} />
          </div>
          {!market ? (
            <EmptyState message="Pick a market to chart it." />
          ) : candles.isError ? (
            <ErrorState message={errorMessage(candles.error)} />
          ) : candles.isPending ? (
            <Skeleton className="min-h-[340px] flex-1 rounded-xl" />
          ) : rows.length === 0 ? (
            <EmptyState message="No trades have printed on this market in the last 1,000 bars of this interval." />
          ) : (
            <div className="min-h-[340px] flex-1">
              <PriceChart
                key={`${market.id}:${interval}`}
                rows={rows}
                style={style}
                priceDecimals={market.priceDecimals}
              />
            </div>
          )}
        </>
      ) : tab === "depth" ? (
        <DepthPanel market={market ?? null} />
      ) : tab === "funding" ? (
        <EmptyState message="No funding feed is published for this market." />
      ) : market ? (
        <Details market={market} />
      ) : (
        <EmptyState message="Pick a market to see its details." />
      )}
    </div>
  );
}

function Details({ market }: { market: Market }) {
  const rows: [string, string][] = [
    ["Market id", market.id],
    ["Venue", market.venue],
    ["Kind", market.kind],
    ["Status", market.status],
    ["Contract", market.address],
    ["Tick size", formatUsd(fromWad(market.tickSize))],
    [
      "Min size",
      `${formatSize(fromWad(market.minSize), 6)} ${
        market.minIsQuoteNotional ? market.quote : market.base
      }`,
    ],
    ["Taker fee", `${market.takerFeeMicros / 10_000}%`],
    ["Maker fee", `${market.makerFeeMicros / 10_000}%`],
  ];
  return (
    <dl className="grid gap-x-8 gap-y-2 sm:grid-cols-2">
      {rows.map(([k, v]) => (
        <div key={k} className="flex items-center justify-between gap-4 text-sm">
          <dt className="text-muted-foreground">{k}</dt>
          <dd className="tnum truncate">{v}</dd>
        </div>
      ))}
    </dl>
  );
}
