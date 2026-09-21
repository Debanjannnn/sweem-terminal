"use client";

import {
  Bar,
  CartesianGrid,
  ComposedChart,
  Line,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { formatUsd, formatUsdCompact, formatSize } from "@/lib/format";

export type Row = {
  t: number;
  o: number;
  h: number;
  l: number;
  c: number;
  v: number;
  range: [number, number];
};

type ShapeProps = {
  x?: number;
  y?: number;
  width?: number;
  height?: number;
  payload?: Row;
};

const UP = "var(--long)";
const DOWN = "var(--short)";

// recharts has no candle primitive: the Bar plots [low, high] so the y scale is
// right, and this shape ignores its rect and redraws open/close off the payload.
function Candle({ x = 0, y = 0, width = 0, height = 0, payload }: ShapeProps) {
  if (!payload) return null;
  const { o, c, h, l } = payload;
  const span = h - l;
  const at = (p: number) => (span === 0 ? y + height / 2 : y + ((h - p) / span) * height);
  const fill = c >= o ? UP : DOWN;
  const bodyTop = at(Math.max(o, c));
  const bodyHeight = Math.max(1, at(Math.min(o, c)) - bodyTop);
  const bodyWidth = Math.max(1, width * 0.6);
  return (
    <g>
      <line
        x1={x + width / 2}
        x2={x + width / 2}
        y1={y}
        y2={y + height}
        stroke={fill}
        strokeWidth={1}
      />
      <rect
        x={x + (width - bodyWidth) / 2}
        y={bodyTop}
        width={bodyWidth}
        height={bodyHeight}
        rx={2}
        fill={fill}
      />
    </g>
  );
}

function VolumeBar({ x = 0, y = 0, width = 0, height = 0, payload }: ShapeProps) {
  if (!payload) return null;
  const w = Math.max(1, width * 0.6);
  return (
    <rect
      x={x + (width - w) / 2}
      y={y}
      width={w}
      height={Math.max(0, height)}
      rx={1}
      fill={payload.c >= payload.o ? UP : DOWN}
      opacity={0.35}
    />
  );
}

const time = (t: number) =>
  new Date(t * 1000).toLocaleString("en-US", {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });

function CandleTooltip({
  active,
  payload,
}: {
  active?: boolean;
  payload?: { payload: Row }[];
}) {
  const row = active ? payload?.[0]?.payload : undefined;
  if (!row) return null;
  return (
    <div className="rounded-xl border border-border bg-popover p-3 text-xs shadow-xl">
      <div className="mb-1.5 text-muted-foreground">{time(row.t)}</div>
      <dl className="tnum grid grid-cols-2 gap-x-4 gap-y-1">
        {(
          [
            ["O", formatUsd(row.o)],
            ["H", formatUsd(row.h)],
            ["L", formatUsd(row.l)],
            ["C", formatUsd(row.c)],
            ["Vol", formatSize(row.v, 2)],
          ] as const
        ).map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="text-muted-foreground">{k}</dt>
            <dd className="text-right">{v}</dd>
          </div>
        ))}
      </dl>
    </div>
  );
}

export function PriceChart({
  rows,
  style = "candle",
}: {
  rows: Row[];
  style?: "candle" | "line";
}) {
  const maxVolume = rows.reduce((m, r) => Math.max(m, r.v), 0);
  return (
    <ResponsiveContainer width="100%" height="100%" minHeight={340}>
      <ComposedChart data={rows} margin={{ top: 8, right: 8, bottom: 0, left: 8 }}>
        <CartesianGrid stroke="var(--border)" vertical={false} />
        <XAxis
          dataKey="t"
          tickFormatter={time}
          minTickGap={48}
          tick={{ fill: "var(--muted-foreground)", fontSize: 11 }}
          axisLine={false}
          tickLine={false}
        />
        <YAxis
          orientation="right"
          domain={["dataMin", "dataMax"]}
          tickFormatter={formatUsdCompact}
          tick={{ fill: "var(--muted-foreground)", fontSize: 11 }}
          axisLine={false}
          tickLine={false}
          width={72}
        />
        {/* volume keeps its own hidden scale so the bars sit in the bottom quarter */}
        <YAxis yAxisId="vol" hide domain={[0, maxVolume * 4 || 1]} />
        <Tooltip content={<CandleTooltip />} cursor={{ stroke: "var(--border)" }} />
        <Bar yAxisId="vol" dataKey="v" shape={<VolumeBar />} isAnimationActive={false} />
        {style === "candle" ? (
          <Bar dataKey="range" shape={<Candle />} isAnimationActive={false} />
        ) : (
          <Line
            dataKey="c"
            type="monotone"
            stroke="var(--primary)"
            strokeWidth={2}
            dot={false}
            isAnimationActive={false}
          />
        )}
      </ComposedChart>
    </ResponsiveContainer>
  );
}
