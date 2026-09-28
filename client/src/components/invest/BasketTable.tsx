"use client";

import { useRouter } from "next/navigation";
import { fmtBps, fmtUsd, fmtUsdCompact, shortHash } from "@/lib/api";
import { Skeleton } from "@/components/ui";
import { TokenIcon } from "@/components/TokenIcon";
import { cn } from "@/lib/utils";
import type { Basket, Market } from "@/lib/types";
import { ChangeText, IconStack, LiquidityBadge } from "./BasketCard";
import { symbolOf, type Extras } from "./extras";

const COLS = [
  { h: "#", c: "hidden sm:table-cell" },
  { h: "Basket", c: "" },
  { h: "Composition", c: "hidden md:table-cell" },
  { h: "NAV / share", c: "text-right" },
  { h: "TVL", c: "hidden sm:table-cell text-right" },
  { h: "24h", c: "text-right" },
  { h: "Creator", c: "hidden lg:table-cell" },
];

export function BasketTable({
  baskets,
  markets,
  extras,
  empty,
  loading,
}: {
  baskets: Basket[];
  markets: Market[] | null;
  extras: Map<string, Extras> | null;
  empty: React.ReactNode;
  loading?: boolean;
}) {
  const router = useRouter();
  return (
    <div className="overflow-x-auto rounded-xl border border-border">
      <table className="w-full">
        <thead>
          <tr className="border-b border-border bg-accent/30">
            {COLS.map(({ h, c }) => (
              <th
                key={h}
                className={cn("px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-muted-foreground", c)}
              >
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {loading ? (
            [0, 1, 2, 3].map((i) => (
              <tr key={i} className="border-b border-border">
                {COLS.map(({ h, c }) => (
                  <td key={h} className={cn("px-4 py-4", c)}>
                    <Skeleton className={h === "Basket" ? "h-8 w-40" : "h-4 w-14"} />
                  </td>
                ))}
              </tr>
            ))
          ) : baskets.length > 0 ? (
            baskets.map((b, i) => {
              const x = extras?.get(b.address);
              const symbols = b.components.map((c) => symbolOf(c.market, markets));
              const nav = Number(b.nav?.navPerShare ?? 0);
              return (
                <tr
                  key={b.address}
                  className="cursor-pointer border-b border-border transition-colors last:border-0 hover:bg-accent/50"
                  onClick={() => router.push(`/baskets/${b.address}`)}
                >
                  <td className={cn("tnum px-4 py-4 text-sm text-muted-foreground", COLS[0].c)}>{i + 1}</td>
                  <td className="px-4 py-4">
                    <div className="flex items-center gap-3">
                      <IconStack symbols={symbols} size={32} ring="ring-background" />
                      <div className="min-w-0">
                        <p className="flex items-center gap-2 text-sm font-medium">
                          <span className="truncate">{b.name}</span>
                          <LiquidityBadge liquid={x?.liquid} />
                        </p>
                        <p className="truncate text-xs text-muted-foreground">{b.symbol}</p>
                      </div>
                    </div>
                  </td>
                  <td className={cn("px-4 py-4", COLS[2].c)}>
                    <div className="flex flex-wrap gap-1.5">
                      {b.components.map((c, j) => (
                        <span
                          key={c.market}
                          className="tnum inline-flex items-center gap-1.5 rounded-md bg-secondary px-2 py-0.5 text-xs text-muted-foreground"
                        >
                          <TokenIcon symbol={symbols[j]} size={14} />
                          {symbols[j]} {fmtBps(c.weightBps)}
                        </span>
                      ))}
                    </div>
                  </td>
                  <td className="tnum px-4 py-4 text-right text-sm font-medium">{nav > 0 ? fmtUsd(nav) : "—"}</td>
                  <td className={cn("tnum px-4 py-4 text-sm text-muted-foreground", COLS[4].c)}>
                    {fmtUsdCompact(Number(b.nav?.totalNav ?? 0))}
                  </td>
                  <td className="tnum px-4 py-4 text-right text-sm">
                    <ChangeText value={x?.change} />
                  </td>
                  <td className={cn("px-4 py-4 text-xs text-muted-foreground", COLS[6].c)}>
                    {b.creator ? shortHash(b.creator) : "—"}
                  </td>
                </tr>
              );
            })
          ) : (
            <tr>
              <td colSpan={COLS.length} className="py-8 text-center text-sm text-muted-foreground">
                {empty}
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}
