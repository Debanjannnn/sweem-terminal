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

// resolution seconds and lookback seconds; resolutions the ingest actually keeps
const RANGES = [
  { value: "1D", label: "1D", resolution: 300, span: 86_400 },
  { value: "7D", label: "7D", resolution: 3_600, span: 7 * 86_400 },
  { value: "1M", label: "1M", resolution: 14_400, span: 30 * 86_400 },
  { value: "3M", label: "3M", resolution: 86_400, span: 90 * 86_400 },
  { value: "1Y", label: "1Y", resolution: 86_400, span: 365 * 86_400 },
  { value: "All", label: "All", resolution: 86_400, span: 1024 * 86_400 },
] as const;

type Tab = (typeof TABS)[number]["value"];
type Style = (typeof STYLES)[number]["value"];
type Range = (typeof RANGES)[number]["value"];

const toRow = (c: Candle): Row => {
  const l = fromWad(c.low);
  const h = fromWad(c.high);
  return {
    t: c.openTime,
    o: fromWad(c.open),
    h,
    l,
    c: fromWad(c.close),
    v: fromWad(c.volume),
    range: [l, h],
  };
};

export function ChartPanel({ market }: { market?: Market | null }) {
  const [tab, setTab] = useState<Tab>("chart");
  const [style, setStyle] = useState<Style>("candle");
  const [range, setRange] = useState<Range>("1D");

  const spec = RANGES.find((r) => r.value === range)!;
  // the window only moves when the range does, so the query key stays stable
  const window = useMemo(() => {
    const to = Math.floor(Date.now() / 1000);
    return { from: to - spec.span, to };
  }, [spec.span]);

  const id = market?.id ?? null;
  const candles = useCandles(id, spec.resolution, window.from, window.to);
  const live = useTopic<Candle>(
    id ? `candle.${id.toLowerCase()}.${spec.resolution}` : null,
  );

  const rows = useMemo(() => {
    const out = (candles.data ?? []).map(toRow);
    if (!live.data || live.data.resolution !== spec.resolution) return out;
    const next = toRow(live.data);
    const last = out[out.length - 1];
    if (last && last.t === next.t) out[out.length - 1] = next;
    else if (!last || next.t > last.t) out.push(next);
    return out;
  }, [candles.data, live.data, spec.resolution]);

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
            <Segmented id="chart-range" value={range} onChange={setRange} options={RANGES} />
          </div>
          {!market ? (
            <EmptyState message="Pick a market to chart it." />
          ) : candles.isError ? (
            <ErrorState message={errorMessage(candles.error)} />
          ) : candles.isPending ? (
            <Skeleton className="min-h-[340px] flex-1 rounded-xl" />
          ) : rows.length === 0 ? (
            <EmptyState message="No candles have been recorded for this market in that range." />
          ) : (
            <div className="min-h-[340px] flex-1">
              <PriceChart rows={rows} style={style} />
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
