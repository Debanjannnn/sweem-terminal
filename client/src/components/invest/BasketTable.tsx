"use client";

import { useRouter } from "next/navigation";
import { fmtBps, fmtUsd, fmtUsdCompact } from "@/lib/api";
import { Skeleton } from "@/components/ui";
import { TokenIcon } from "@/components/TokenIcon";
import { cn } from "@/lib/utils";
import type { Basket, Market } from "@/lib/types";
import { ChangeText, IconStack, LiquidityBadge } from "./BasketCard";
import { symbolOf, type Extras } from "./extras";

const COLS = [
  { h: "#", c: "hidden w-12 pl-6 sm:table-cell" },
  { h: "Basket", c: "pl-4 md:pl-6" },
  { h: "Composition", c: "hidden lg:table-cell" },
  { h: "Price", c: "text-right" },
  { h: "24h", c: "text-right" },
  { h: "TVL", c: "hidden sm:table-cell text-right" },
  { h: "Status", c: "hidden md:table-cell pr-6 md:pr-8 text-right" },
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
    <div className="overflow-hidden rounded-3xl border border-border bg-card shadow-[0_8px_40px_rgba(0,0,0,0.25)]">
      <div className="overflow-x-auto">
        <table className="w-full border-collapse text-left">
          <thead>
            <tr className="border-b border-border bg-secondary/40">
              {COLS.map(({ h, c }) => (
                <th key={h} className={cn("px-3 py-3 text-[13px] font-medium text-muted-foreground md:px-4", c)}>
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {loading ? (
              [0, 1, 2, 3].map((i) => (
                <tr key={i} className="h-16">
                  {COLS.map(({ h, c }) => (
                    <td key={h} className={cn("px-3 md:px-4", c)}>
                      <Skeleton className={cn(h === "Basket" ? "h-8 w-40" : "h-4 w-14", c.includes("text-right") && "ml-auto")} />
                    </td>
                  ))}
                </tr>
              ))
            ) : baskets.length > 0 ? (
              baskets.map((b, i) => {
                const x = extras?.get(b.address);
                const symbols = b.components.map((c) => symbolOf(c.market, markets));
                const nav = Number(b.nav?.navPerShare ?? 0);
                const open = () => router.push(`/baskets/${b.address}`);
                return (
                  <tr
                    key={b.address}
                    tabIndex={0}
                    className="h-16 cursor-pointer transition-colors hover:bg-secondary/40 focus-visible:bg-secondary/40 focus-visible:outline-none"
                    onClick={open}
                    onKeyDown={(e) => e.key === "Enter" && open()}
                  >
                    <td className={cn("tnum px-3 text-sm text-muted-foreground md:px-4", COLS[0].c)}>{i + 1}</td>
                    <td className={cn("px-3 md:px-4", COLS[1].c)}>
                      <div className="flex items-center gap-3">
                        <IconStack symbols={symbols} size={28} ring="ring-card" />
                        <span className="truncate text-[15px] font-medium">{b.name}</span>
                        <span className="shrink-0 text-sm text-muted-foreground">{b.symbol}</span>
                      </div>
                    </td>
                    <td className={cn("px-3 md:px-4", COLS[2].c)}>
                      <div className="flex flex-wrap gap-1.5">
                        {b.components.map((c, j) => (
                          <span
                            key={c.market}
                            className="tnum inline-flex items-center gap-1.5 rounded-full bg-secondary px-2 py-0.5 text-[11px] font-medium text-muted-foreground"
                          >
                            <TokenIcon symbol={symbols[j]} size={14} />
                            {symbols[j]} {fmtBps(c.weightBps).replace(".00", "")}
                          </span>
                        ))}
                      </div>
                    </td>
                    <td className="tnum px-3 text-right text-sm font-medium md:px-4">{nav > 0 ? fmtUsd(nav) : "—"}</td>
                    <td className="px-3 text-right text-sm md:px-4">
                      <ChangeText value={x?.change} className="justify-end" />
                    </td>
                    <td className={cn("tnum px-3 text-sm font-medium md:px-4", COLS[5].c)}>
                      {fmtUsdCompact(Number(b.nav?.totalNav ?? 0))}
                    </td>
                    <td className={cn("px-3 md:px-4", COLS[6].c)}>
                      <LiquidityBadge liquid={x?.liquid} />
                    </td>
                  </tr>
                );
              })
            ) : (
              <tr>
                <td colSpan={COLS.length} className="py-10 text-center text-sm text-muted-foreground">
                  {empty}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
