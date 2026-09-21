"use client";

import { Settings2 } from "lucide-react";
import { Skeleton } from "@/components/ui/Skeleton";
import { formatPctSigned, formatUsd, formatUsdCompact } from "@/lib/format";
import { cn } from "@/lib/cn";
import { MarketPicker, type MarketOption } from "./MarketPicker";

// every figure is optional: a missing mark is omitted, never shown as zero
export type Market = {
  symbol: string;
  price?: number;
  markPrice?: number;
  indexPrice?: number;
  changePct?: number;
  high24h?: number;
  low24h?: number;
  volume24h?: number;
};

export function MarketHeader({
  market,
  markets = [],
  onSelect,
}: {
  market: Market | null;
  markets?: MarketOption[];
  onSelect?: (symbol: string) => void;
}) {
  // undefined = still loading, null = the feed has no value for it
  const show = <T,>(v: T | undefined, fmt: (v: T) => string) =>
    market ? (v === undefined || (typeof v === "number" && Number.isNaN(v)) ? null : fmt(v)) : undefined;

  return (
    <div className="flex flex-wrap items-center gap-x-8 gap-y-4 p-4">
      <div className="flex items-center gap-4">
        <MarketPicker
          value={market?.symbol ?? null}
          markets={markets}
          onChange={onSelect}
        />
        {market ? (
          <span className="tnum text-2xl font-semibold">
            {market.price === undefined || Number.isNaN(market.price)
              ? "—"
              : formatUsd(market.price)}
          </span>
        ) : (
          <Skeleton className="h-7 w-32" />
        )}
      </div>

      <Stat label="Mark" value={show(market?.markPrice, formatUsd)} />
      <Stat label="Index" value={show(market?.indexPrice, formatUsd)} />
      <Stat
        label="24h change"
        value={show(market?.changePct, (v) => formatPctSigned(v))}
        tone={
          market?.changePct === undefined ? undefined : market.changePct >= 0 ? "long" : "short"
        }
      />
      <Stat
        label="24h high / low"
        value={
          market && market.high24h !== undefined && market.low24h !== undefined
            ? `${formatUsd(market.high24h)} / ${formatUsd(market.low24h)}`
            : market
              ? null
              : undefined
        }
      />
      <Stat label="24h volume" value={show(market?.volume24h, formatUsdCompact)} />

      <button
        type="button"
        aria-label="Market settings"
        className="ml-auto rounded-full p-2.5 text-muted-foreground transition-colors hover:text-foreground"
      >
        <Settings2 className="size-5" />
      </button>
    </div>
  );
}

function Stat({
  label,
  value,
  tone,
}: {
  label: string;
  value: string | null | undefined;
  tone?: "long" | "short";
}) {
  return (
    <div className="space-y-1">
      <div className="text-[11px] uppercase tracking-wide text-muted-foreground">
        {label}
      </div>
      {value === undefined ? (
        <Skeleton className="h-4 w-20" />
      ) : (
        <div
          className={cn(
            "tnum text-sm",
            value !== null && tone === "long" && "text-long",
            value !== null && tone === "short" && "text-short",
          )}
        >
          {value ?? "—"}
        </div>
      )}
    </div>
  );
}
