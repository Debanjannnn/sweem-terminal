"use client";

import { useEffect, useRef, useState } from "react";
import {
  AreaSeries,
  CandlestickSeries,
  ColorType,
  createChart,
  CrosshairMode,
  HistogramSeries,
  LineStyle,
  type IChartApi,
  type ISeriesApi,
  type UTCTimestamp,
} from "lightweight-charts";
import { fmtNum, fmtUsd, fmtUsdCompact } from "@/lib/api";
import type { Interval } from "@/lib/types";
import { ErrorBox, Skeleton } from "@/components/ui";
import { ChangeText } from "@/components/IconStack";
import { cn } from "@/lib/utils";
import { Flash, Segmented, TabPill } from "@/components/motion";

const H = 3600e3;
const D = 24 * H;
// ms null = everything the feed has (market-data caps at 1000 bars)
export const RANGES = {
  "1H": { ms: H, iv: "1m" },
  "1D": { ms: D, iv: "5m" },
  "1W": { ms: 7 * D, iv: "1h" },
  "1M": { ms: 30 * D, iv: "1h" },
  ALL: { ms: null, iv: "1d" },
} as const satisfies Record<string, { ms: number | null; iv: Interval }>;
export type Range = keyof typeof RANGES;

export type Point = { t: number; v: number; o?: number; h?: number; l?: number; vol?: number };

// canvas colours: sweem theme tokens resolved to hex (lightweight-charts draws on canvas, no CSS vars)
const UP = "#59c5bf";
const DOWN = "#d55f59";
const LIME = "#c4f56a";
const MUTED = "#929292";
const GRID = "rgba(255,255,255,0.04)";

// chart runs in UTC; shift so axis + crosshair read local time
const toTime = (t: number) => Math.floor(t / 1000 - new Date(t).getTimezoneOffset() * 60) as UTCTimestamp;
const fmtPx = (p: number) => fmtNum(p, Math.abs(p) >= 1000 ? 2 : Math.abs(p) >= 1 ? 4 : 6);

type Api = { chart: IChartApi; main: ISeriesApi<"Candlestick"> | ISeriesApi<"Area">; vol: ISeriesApi<"Histogram">; key: string; n: number };

// TradingView lightweight-charts: candles + volume, or area line; live frames update the last bar in place.
function TvChart({ points, candle, color, dataKey, valueLabel }: { points: Point[]; candle: boolean; color: string; dataKey: string; valueLabel: string }) {
  const box = useRef<HTMLDivElement>(null);
  const api = useRef<Api | null>(null);
  const byTime = useRef(new Map<number, Point>());
  const [hover, setHover] = useState<Point | null>(null);

  useEffect(() => {
    const chart = createChart(box.current!, {
      autoSize: true,
      layout: { background: { type: ColorType.Solid, color: "transparent" }, textColor: MUTED, fontSize: 11, fontFamily: getComputedStyle(document.body).fontFamily },
      grid: { vertLines: { color: GRID }, horzLines: { color: GRID } },
      crosshair: {
        mode: CrosshairMode.Normal,
        vertLine: { color: "rgba(255,255,255,0.25)", style: LineStyle.Dashed, labelBackgroundColor: "#2a2a2a" },
        horzLine: { color: "rgba(255,255,255,0.25)", style: LineStyle.Dashed, labelBackgroundColor: "#2a2a2a" },
      },
      rightPriceScale: { borderVisible: false, scaleMargins: { top: 0.1, bottom: candle ? 0.22 : 0.08 } },
      timeScale: { borderVisible: false, timeVisible: true, secondsVisible: false, rightOffset: 4 },
      localization: { priceFormatter: fmtPx },
    });
    const main = candle
      ? chart.addSeries(CandlestickSeries, { upColor: UP, downColor: DOWN, borderVisible: false, wickUpColor: UP, wickDownColor: DOWN })
      : chart.addSeries(AreaSeries, { lineWidth: 2 });
    const vol = chart.addSeries(HistogramSeries, { priceScaleId: "", priceFormat: { type: "volume" }, lastValueVisible: false, priceLineVisible: false, visible: candle });
    vol.priceScale().applyOptions({ scaleMargins: { top: 0.82, bottom: 0 } });
    chart.subscribeCrosshairMove((p) => setHover(p.time ? (byTime.current.get(p.time as number) ?? null) : null));
    api.current = { chart, main, vol, key: "", n: 0 };
    return () => {
      api.current = null;
      chart.remove();
    };
  }, [candle]);

  useEffect(() => {
    if (!candle) (api.current?.main as ISeriesApi<"Area"> | undefined)?.applyOptions({ lineColor: color, topColor: `${color}40`, bottomColor: `${color}00` });
  }, [candle, color]);

  useEffect(() => {
    const a = api.current;
    if (!a) return;
    const mainOf = (p: Point) =>
      candle ? { time: toTime(p.t), open: p.o ?? p.v, high: p.h ?? p.v, low: p.l ?? p.v, close: p.v } : { time: toTime(p.t), value: p.v };
    const volOf = (p: Point) => ({ time: toTime(p.t), value: p.vol ?? 0, color: p.v >= (p.o ?? p.v) ? `${UP}55` : `${DOWN}55` });
    byTime.current = new Map(points.map((p) => [toTime(p.t), p]));
    const last = points.at(-1);
    // tick precision follows magnitude (MON ≈ $0.03 needs 6 decimals, WETH 2)
    const precision = last && Math.abs(last.v) < 1 ? 6 : last && Math.abs(last.v) < 1000 ? 4 : 2;
    a.main.applyOptions({ priceFormat: { type: "price", precision, minMove: 10 ** -precision } });
    // same series, at most one new bar: live tick
    if (last && a.key === dataKey && a.n > 0 && (points.length === a.n || points.length === a.n + 1)) {
      (a.main as ISeriesApi<"Candlestick">).update(mainOf(last) as never);
      a.vol.update(volOf(last));
    } else {
      (a.main as ISeriesApi<"Candlestick">).setData(points.map(mainOf) as never);
      a.vol.setData(points.map(volOf));
      // few bars: keep a normal bar width, right-aligned, instead of stretching them across the pane
      if (points.length >= 50) a.chart.timeScale().fitContent();
      else a.chart.timeScale().setVisibleLogicalRange({ from: points.length - 50, to: points.length + 3 });
    }
    a.key = dataKey;
    a.n = points.length;
  }, [points, candle, dataKey]);

  const h = hover;
  const up = h ? h.v >= (h.o ?? h.v) : true;
  return (
    <div className="relative size-full">
      <div ref={box} className="size-full" />
      {h && (
        <div className="tnum pointer-events-none absolute left-1 top-0 z-10 flex flex-wrap gap-x-3 font-mono text-[11px] text-muted-foreground">
          {candle ? (
            <>
              {(["o", "h", "l"] as const).map((k) => (
                <span key={k}>
                  {k.toUpperCase()} <span className={up ? "text-positive" : "text-destructive"}>{fmtPx(h[k] ?? h.v)}</span>
                </span>
              ))}
              <span>
                C <span className={up ? "text-positive" : "text-destructive"}>{fmtPx(h.v)}</span>
              </span>
              {h.vol !== undefined && <span>Vol {fmtUsdCompact(h.vol)}</span>}
            </>
          ) : (
            <span>
              {valueLabel} <span className="text-foreground">{fmtPx(h.v)}</span>
            </span>
          )}
        </div>
      )}
    </div>
  );
}

// Price card: live headline + range change + TradingView chart; tint follows direction.
export function PriceChart({
  points,
  price,
  range,
  onRange,
  mode,
  onMode,
  loading,
  error,
  caption,
  dataKey,
  empty = "No trades in this range yet.",
}: {
  points: Point[];
  price: number | null;
  range: Range;
  onRange: (r: Range) => void;
  mode?: "line" | "candle";
  onMode?: (m: "line" | "candle") => void;
  loading: boolean;
  error: string | null;
  caption: string;
  dataKey: string;
  empty?: string;
}) {
  const first = points[0]?.o ?? points[0]?.v;
  const last = price ?? points.at(-1)?.v ?? null;
  const change = points.length > 0 && first && last ? (last / first - 1) * 100 : null;
  const down = change !== null && Number(change.toFixed(2)) < 0;

  return (
    <section className={cn("rounded-[32px] p-6", down ? "bg-destructive/[0.07]" : "bg-primary/[0.06]")}>
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <div className="tnum text-5xl font-medium tracking-tight">
            {last !== null ? <Flash value={last}>{fmtUsd(last)}</Flash> : loading ? <Skeleton className="h-12 w-48" /> : "-"}
          </div>
          <div className="mt-2 text-sm">
            {change !== null && first && last ? (
              <span className="inline-flex items-center gap-1.5">
                <ChangeText value={change} className="gap-1.5" />
                <span className={cn("font-mono", down ? "text-destructive" : "text-positive")}>
                  ({last - first >= 0 ? "+" : "-"}
                  {fmtUsd(Math.abs(last - first))}) {range}
                </span>
              </span>
            ) : (
              <span className="text-muted-foreground">{caption}</span>
            )}
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {onMode && mode && (
            <Segmented
              id="chart-mode"
              className="w-auto"
              value={mode}
              onChange={onMode}
              options={[
                { value: "candle", label: "Candles" },
                { value: "line", label: "Line" },
              ]}
            />
          )}
          <div role="tablist" aria-label="Chart range" className="flex rounded-lg bg-foreground/[0.05] p-0.5">
            {(Object.keys(RANGES) as Range[]).map((r) => (
              <button
                key={r}
                role="tab"
                aria-selected={r === range}
                onClick={() => onRange(r)}
                className={cn(
                  "relative rounded-md px-2.5 py-1 text-sm font-medium transition-colors",
                  r === range ? "text-foreground" : "text-muted-foreground hover:text-foreground",
                )}
              >
                {r === range && <TabPill id="chart-range" className="rounded-md bg-card shadow-sm" />}
                <span className="relative">{r}</span>
              </button>
            ))}
          </div>
        </div>
      </div>
      <div className="mt-6 h-80">
        {error ? (
          <ErrorBox message={error} />
        ) : loading && points.length === 0 ? (
          <Skeleton className="h-full bg-foreground/[0.04]" />
        ) : points.length === 0 ? (
          <div className="grid h-full place-items-center text-sm text-muted-foreground">{empty}</div>
        ) : (
          <TvChart points={points} candle={mode === "candle"} color={down ? DOWN : LIME} dataKey={dataKey} valueLabel={caption} />
        )}
      </div>
    </section>
  );
}
