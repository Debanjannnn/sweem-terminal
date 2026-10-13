"use client";

import { fmtNum, fmtUsd } from "@/lib/api";
import { mid } from "@/lib/hooks";
import type { Book, Level } from "@/lib/types";
import { Panel, Skeleton } from "@/components/ui";

const N = 8;

// live top-of-book ladder; depth bars scaled to the largest level shown
export function OrderBook({ book, symbol, quote = "USDC", stale, error }: { book: Book | null | undefined; symbol: string; quote?: string; stale: boolean; error: string | null }) {
  const asks = book?.asks.slice(0, N).reverse() ?? [];
  const bids = book?.bids.slice(0, N) ?? [];
  const max = Math.max(...[...asks, ...bids].map((l) => Number(l[1])), 0);
  const m = mid(book);
  const spread = book?.asks[0] && book?.bids[0] ? Number(book.asks[0][0]) - Number(book.bids[0][0]) : null;
  const row = (l: Level, ask: boolean) => (
    <div key={`${ask}${l[0]}`} className="relative grid grid-cols-2 px-6 py-1 text-xs">
      <div
        className={ask ? "absolute inset-y-0 right-0 bg-destructive/10" : "absolute inset-y-0 right-0 bg-positive/10"}
        style={{ width: `${max ? (Number(l[1]) / max) * 100 : 0}%` }}
      />
      <span className={`tnum relative ${ask ? "text-destructive" : "text-positive"}`}>{fmtUsd(Number(l[0]))}</span>
      <span className="tnum relative text-right">{fmtNum(Number(l[1]), 6)}</span>
    </div>
  );
  return (
    <Panel>
      <div className="flex items-center justify-between px-6 pb-3 pt-6">
        <h2 className="text-2xl font-medium">Order book</h2>
        {stale && <span className="rounded-full bg-warning/10 px-2 py-0.5 text-[11px] font-medium text-warning">Stale</span>}
      </div>
      <div className="grid grid-cols-2 border-y border-border px-6 py-2 text-[11px] text-muted-foreground">
        <span>Price ({quote})</span>
        <span className="text-right">Size ({symbol})</span>
      </div>
      {error && !book ? (
        <p className="px-6 py-8 text-center text-sm text-muted-foreground">Book unavailable ({error}).</p>
      ) : !book ? (
        <div className="space-y-2 p-6">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-4 w-full" />
          ))}
        </div>
      ) : (
        <div className="py-2">
          {asks.map((l) => row(l, true))}
          <div className="tnum flex justify-between border-y border-border px-6 py-2 text-sm">
            <span className="font-medium">{m !== null ? fmtUsd(m) : "-"}</span>
            <span className="text-xs text-muted-foreground">spread {spread !== null ? fmtUsd(spread) : "-"}</span>
          </div>
          {bids.map((l) => row(l, false))}
          {!asks.length && !bids.length && <p className="py-6 text-center text-sm text-muted-foreground">Empty book.</p>}
        </div>
      )}
    </Panel>
  );
}
