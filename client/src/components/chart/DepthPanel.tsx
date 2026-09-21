"use client";

import { ApiError, fromWad, type Book, type Level, type Market } from "@/lib/api";
import { formatSize, formatUsd } from "@/lib/format";
import { useBook } from "@/lib/query";
import { useTopic } from "@/lib/ws";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { Skeleton } from "@/components/ui/Skeleton";
import { cn } from "@/lib/cn";

const ROWS = 12;

export function DepthPanel({ market }: { market: Market | null }) {
  const rest = useBook(market?.id ?? null);
  const live = useTopic<Book>(
    market ? `book.${market.venue}.${market.id.toLowerCase()}` : null,
  );
  const book = live.data ?? rest.data;

  if (!market) return <EmptyState message="Pick a market to see its book." />;
  // a cold book is a real state; an empty book would be a lie
  if (!book && rest.error instanceof ApiError && rest.error.code === "book_cold") {
    return <ErrorState message={rest.error.message} />;
  }
  if (!book && rest.isError) return <ErrorState message={(rest.error as Error).message} />;
  if (!book) {
    return (
      <div className="space-y-2">
        {Array.from({ length: 6 }, (_, i) => (
          <Skeleton key={i} className="h-6" />
        ))}
      </div>
    );
  }

  const bids = book.bids.slice(0, ROWS);
  const asks = book.asks.slice(0, ROWS);
  const max = Math.max(
    ...[...bids, ...asks].map((l) => fromWad(l.size)),
    Number.MIN_VALUE,
  );

  return (
    <div className="grid gap-6 sm:grid-cols-2">
      <Side title="Bids" levels={bids} max={max} tone="long" base={market.base} />
      <Side title="Asks" levels={asks} max={max} tone="short" base={market.base} />
    </div>
  );
}

function Side({
  title,
  levels,
  max,
  tone,
  base,
}: {
  title: string;
  levels: Level[];
  max: number;
  tone: "long" | "short";
  base: string;
}) {
  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between text-[11px] uppercase tracking-wide text-muted-foreground">
        <span>{title}</span>
        <span>Size ({base})</span>
      </div>
      {levels.length === 0 ? (
        <EmptyState message={`No resting ${title.toLowerCase()}.`} />
      ) : (
        levels.map((l) => {
          const size = fromWad(l.size);
          return (
            <div key={l.price} className="relative flex justify-between px-2 py-1 text-xs">
              <span
                aria-hidden
                style={{ width: `${(size / max) * 100}%` }}
                className={cn(
                  "absolute inset-y-0 right-0 rounded-sm opacity-15",
                  tone === "long" ? "bg-long" : "bg-short",
                )}
              />
              <span
                className={cn("tnum relative", tone === "long" ? "text-long" : "text-short")}
              >
                {formatUsd(fromWad(l.price))}
              </span>
              <span className="tnum relative">{formatSize(size)}</span>
            </div>
          );
        })
      )}
    </div>
  );
}
