"use client";

import { useEffect, useRef, useState } from "react";
import {
  CandlestickSeries,
  ColorType,
  CrosshairMode,
  HistogramSeries,
  LineSeries,
  createChart,
  type IChartApi,
  type ISeriesApi,
  type MouseEventParams,
  type Time,
  type UTCTimestamp,
} from "lightweight-charts";
import { formatSize } from "@/lib/format";
import { cn } from "@/lib/cn";

export type Row = {
  /** bar open, unix seconds */
  t: number;
  o: number;
  h: number;
  l: number;
  c: number;
  v: number;
};

export type ChartStyle = "candle" | "line";

// lightweight-charts parses a handful of color syntaxes and the theme is in
// oklch, so each token is painted once and read back as rgba
function cssColor(name: string, fallback: string): string {
  if (typeof document === "undefined") return fallback;
  const raw = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  if (!raw) return fallback;
  const ctx = document.createElement("canvas").getContext("2d");
  if (!ctx) return fallback;
  ctx.fillStyle = fallback;
  ctx.fillStyle = raw;
  ctx.fillRect(0, 0, 1, 1);
  const [r, g, b, a] = ctx.getImageData(0, 0, 1, 1).data;
  return `rgba(${r}, ${g}, ${b}, ${(a / 255).toFixed(3)})`;
}

const withAlpha = (rgba: string, alpha: number) => rgba.replace(/[\d.]+\)$/, `${alpha})`);

type Legend = { row: Row; prev?: Row };

export function PriceChart({
  rows,
  style,
  priceDecimals,
}: {
  rows: Row[];
  style: ChartStyle;
  /** the venue's price precision, so the axis never shows digits the book cannot hold */
  priceDecimals: number;
}) {
  const box = useRef<HTMLDivElement>(null);
  const chart = useRef<IChartApi | null>(null);
  const price = useRef<ISeriesApi<"Candlestick"> | ISeriesApi<"Line"> | null>(null);
  const volume = useRef<ISeriesApi<"Histogram"> | null>(null);
  const loaded = useRef<{ first: number; count: number } | null>(null);
  const colors = useRef({ up: "", down: "" });
  const [legend, setLegend] = useState<Legend | null>(null);

  // one chart per mount; the parent remounts on a new market or interval
  useEffect(() => {
    const el = box.current;
    if (!el) return;
    const up = cssColor("--long", "#c6f36b");
    const down = cssColor("--short", "#e5484d");
    const text = cssColor("--muted-foreground", "#a1a1a1");
    const grid = cssColor("--border", "rgba(255,255,255,0.06)");
    colors.current = { up, down };

    const c = createChart(el, {
      autoSize: true,
      layout: {
        background: { type: ColorType.Solid, color: "transparent" },
        textColor: text,
        fontFamily: getComputedStyle(el).fontFamily,
        fontSize: 11,
        attributionLogo: true,
        panes: { separatorColor: grid, separatorHoverColor: grid, enableResize: true },
      },
      grid: { vertLines: { color: grid }, horzLines: { color: grid } },
      crosshair: {
        mode: CrosshairMode.Normal,
        vertLine: { color: text, labelBackgroundColor: withAlpha(text, 0.9) },
        horzLine: { color: text, labelBackgroundColor: withAlpha(text, 0.9) },
      },
      rightPriceScale: { borderColor: grid },

      timeScale: { borderColor: grid, timeVisible: true, secondsVisible: false, rightOffset: 6 },
    });
    chart.current = c;

    // grouped digits like the header and legend; scoped to price so volume keeps its own format
    const format = {
      type: "custom" as const,
      minMove: 10 ** -priceDecimals,
      formatter: (n: number) =>
        n.toLocaleString("en-US", { minimumFractionDigits: priceDecimals, maximumFractionDigits: priceDecimals }),
    };
    price.current =
      style === "candle"
        ? c.addSeries(CandlestickSeries, {
            upColor: up,
            downColor: down,
            borderUpColor: up,
            borderDownColor: down,
            wickUpColor: up,
            wickDownColor: down,
            priceFormat: format,
          })
        : c.addSeries(LineSeries, { color: up, lineWidth: 2, priceFormat: format });
    // volume gets its own pane, as on a trading terminal
    volume.current = c.addSeries(
      HistogramSeries,
      { priceFormat: { type: "volume" }, lastValueVisible: false, priceLineVisible: false },
      1,
    );
    c.panes()[1]?.setHeight(90);

    const onMove = (p: MouseEventParams<Time>) => {
      const bar = p.time !== undefined && price.current ? p.seriesData.get(price.current) : undefined;
      if (!bar) {
        setLegend(null);
        return;
      }
      setLegend(latest.current.find(p.time as number));
    };
    c.subscribeCrosshairMove(onMove);

    return () => {
      c.unsubscribeCrosshairMove(onMove);
      c.remove();
      chart.current = price.current = volume.current = null;
      loaded.current = null;
    };
  }, [style, priceDecimals]);

  // the crosshair callback reads the newest rows without re-subscribing
  const latest = useRef({ find: (_t: number): Legend | null => null });
  latest.current.find = (t: number) => {
    const i = rows.findIndex((r) => r.t === t);
    return i < 0 ? null : { row: rows[i], prev: rows[i - 1] };
  };

  // a live bar is an update; anything else reloads the series
  useEffect(() => {
    const p = price.current;
    const v = volume.current;
    if (!p || !v || rows.length === 0) return;
    const { up, down } = colors.current;
    const toBar = (r: Row) =>
      style === "candle"
        ? { time: r.t as UTCTimestamp, open: r.o, high: r.h, low: r.l, close: r.c }
        : { time: r.t as UTCTimestamp, value: r.c };
    const toVol = (r: Row) => ({
      time: r.t as UTCTimestamp,
      value: r.v,
      color: withAlpha(r.c >= r.o ? up : down, 0.45),
    });

    const prev = loaded.current;
    const sameHistory = prev && prev.first === rows[0].t && rows.length - prev.count <= 1 && rows.length >= prev.count;
    if (sameHistory) {
      const last = rows[rows.length - 1];
      (p as ISeriesApi<"Candlestick">).update(toBar(last) as never);
      v.update(toVol(last));
    } else {
      (p as ISeriesApi<"Candlestick">).setData(rows.map(toBar) as never);
      v.setData(rows.map(toVol));
      if (!prev) chart.current?.timeScale().fitContent();
    }
    loaded.current = { first: rows[0].t, count: rows.length };
  }, [rows, style]);

  const shown = legend ?? (rows.length ? { row: rows[rows.length - 1], prev: rows[rows.length - 2] } : null);
  const change = shown?.prev ? (shown.row.c / shown.prev.c - 1) * 100 : undefined;
  const fmt = (n: number) => n.toLocaleString("en-US", { minimumFractionDigits: priceDecimals, maximumFractionDigits: priceDecimals });

  return (
    <div className="relative h-full w-full">
      {shown && (
        <div className="tnum pointer-events-none absolute top-1 left-2 z-10 flex flex-wrap gap-x-3 text-[11px] text-muted-foreground">
          {(["o", "h", "l", "c"] as const).map((k) => (
            <span key={k}>
              {k.toUpperCase()}{" "}
              <span className={cn(shown.row.c >= shown.row.o ? "text-long" : "text-short")}>
                {fmt(shown.row[k])}
              </span>
            </span>
          ))}
          {change !== undefined && (
            <span className={change >= 0 ? "text-long" : "text-short"}>
              {change >= 0 ? "+" : ""}
              {change.toFixed(2)}%
            </span>
          )}
          <span>Vol {formatSize(shown.row.v, 2)}</span>
        </div>
      )}
      <div ref={box} className="h-full w-full" />
    </div>
  );
}
