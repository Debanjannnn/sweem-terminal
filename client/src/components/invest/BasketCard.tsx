"use client";

import Link from "next/link";
import { fmtPctSigned, fmtUsd, fmtUsdCompact } from "@/lib/api";
import { useTopic } from "@/lib/ws";
import { TokenIcon } from "@/components/TokenIcon";
import { cn } from "@/lib/utils";
import type { Basket, LiveNav, Market } from "@/lib/types";
import { symbolOf, type Extras } from "./extras";

export function IconStack({ symbols, size, ring }: { symbols: string[]; size: number; ring: string }) {
  return (
    <span aria-hidden className="flex shrink-0 -space-x-2">
      {symbols.slice(0, 3).map((s, i) => (
        <TokenIcon
          key={`${s}${i}`}
          symbol={s}
          size={size}
          className={cn("rounded-full ring-2 transition-transform duration-200 ease-out hover:z-10 hover:-translate-y-1", ring)}
        />
      ))}
    </span>
  );
}

export function ChangeText({ value, className }: { value: number | null | undefined; className?: string }) {
  if (value === null || value === undefined) return <span className={cn("text-muted-foreground", className)}>—</span>;
  return (
    <span className={cn(value < 0 ? "text-destructive" : "text-positive", className)}>{fmtPctSigned(value)}</span>
  );
}

export function LiquidityBadge({ liquid }: { liquid: boolean | null | undefined }) {
  if (liquid !== false) return null;
  return (
    <span className="rounded-md border border-warning/30 bg-warning/10 px-2 py-0.5 text-[11px] whitespace-nowrap text-warning">
      no liquidity
    </span>
  );
}

// live NAV via WS nav.<basket>, falls back to the indexed snapshot
export function BasketCard({ basket, markets, extras }: { basket: Basket; markets: Market[] | null; extras?: Extras }) {
  const live = useTopic<LiveNav>(`nav.${basket.address}`);
  const nav = Number(live?.navPerShare ?? basket.nav?.navPerShare ?? 0);
  const tvl = Number(live?.totalNav ?? basket.nav?.totalNav ?? 0);
  const symbols = basket.components.map((c) => symbolOf(c.market, markets));
  return (
    <Link
      href={`/baskets/${basket.address}`}
      className="flex w-[270px] shrink-0 flex-col justify-between gap-6 rounded-xl border border-border bg-card p-5 transition-colors hover:border-input"
    >
      <div className="flex items-start gap-3">
        <IconStack symbols={symbols} size={36} ring="ring-card" />
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-semibold">{basket.name}</p>
          <p className="truncate text-xs text-muted-foreground">{symbols.join(" · ")}</p>
        </div>
        <LiquidityBadge liquid={extras?.liquid} />
      </div>
      <div>
        <div className="flex items-baseline gap-2">
          <span className="tnum text-3xl font-medium tracking-tight">{nav > 0 ? fmtUsd(nav) : "—"}</span>
          <span className="text-sm text-muted-foreground">/ share</span>
          {live && (
            <span aria-label="live" className="ml-auto size-1.5 animate-pulse rounded-full bg-primary" />
          )}
        </div>
        <div className="tnum mt-1 flex items-center gap-3 text-xs text-muted-foreground">
          <span>TVL {fmtUsdCompact(tvl)}</span>
          <ChangeText value={extras?.change} />
          <span className="ml-auto">{basket.symbol}</span>
        </div>
      </div>
    </Link>
  );
}
