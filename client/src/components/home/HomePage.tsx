"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { ChevronDown, ChevronUp, ChevronsUpDown } from "lucide-react";
import { fmtUsd, fmtUsdCompact, getBaskets } from "@/lib/api";
import { useAsync, useSession } from "@/lib/session";
import { tokenBySymbol } from "@/lib/tokens";
import { subscribe } from "@/lib/ws";
import { cn } from "@/lib/utils";
import type { Basket, LiveNav } from "@/lib/types";
import { ErrorBox, Skeleton } from "@/components/ui";
import { HeroSearch } from "@/components/BasketSearch";
import { Sparkline } from "@/components/Sparkline";
import { ChangeText, IconStack, LiquidityBadge } from "@/components/invest/BasketCard";
import { symbolOf, useBasketExtras, useMarkets } from "@/components/invest/extras";

type Row = {
  b: Basket;
  symbols: string[];
  series: number[];
  price: number | null;
  chgUsd: number | null;
  chg: number | null;
  tvl: number;
  liquid: boolean | null | undefined;
};

// tab label per component token; unknown tokens fall back to their name
const LABEL: Record<string, string> = { cbBTC: "Bitcoin", WETH: "Ether", XAUT: "Gold", MON: "Monad" };

function useLiveNavs(baskets: Basket[] | null) {
  const [live, setLive] = useState<Record<string, LiveNav>>({});
  const key = baskets?.map((b) => b.address).join() ?? "";
  useEffect(() => {
    if (!key) return;
    const offs = key.split(",").map((a) => subscribe(`nav.${a}`, (d) => setLive((m) => ({ ...m, [a]: d as LiveNav }))));
    return () => offs.forEach((off) => off());
  }, [key]);
  return live;
}

export function HomePage() {
  const { authenticated, address } = useSession();
  const all = useAsync("baskets", () => getBaskets());
  const markets = useMarkets();
  const extras = useBasketExtras(all.data);
  const live = useLiveNavs(all.data);
  const [tab, setTab] = useState("all");
  const [sort, setSort] = useState<{ key: SortKey; desc: boolean } | null>({ key: "tvl", desc: true });

  const rows: Row[] = (all.data ?? []).map((b) => {
    const x = extras?.get(b.address);
    const l = live[b.address];
    const price = Number(l?.navPerShare ?? b.nav?.navPerShare ?? 0) || null;
    const series = x ? (l && price ? [...x.hist, price] : x.hist) : [];
    const first = series[0];
    const last = series[series.length - 1];
    const ok = series.length > 1 && first > 0;
    return {
      b,
      symbols: b.components.map((c) => symbolOf(c.market, markets.data)),
      series,
      price,
      chgUsd: ok ? last - first : null,
      chg: ok ? ((last - first) / first) * 100 : null,
      tvl: Number(l?.totalNav ?? b.nav?.totalNav ?? 0),
      liquid: x?.liquid,
    };
  });

  const cats = [...new Set(rows.flatMap((r) => r.symbols))].filter((s) => s !== "…");
  const active = (tab === "mine" && !authenticated) || (tab !== "all" && tab !== "mine" && !cats.includes(tab)) ? "all" : tab;
  const shown = rows.filter((r) =>
    active === "all" ? true : active === "mine" ? r.b.creator === address?.toLowerCase() : r.symbols.includes(active),
  );
  if (sort) {
    const v = (r: Row) => (sort.key === "name" ? r.b.name : r[sort.key]);
    shown.sort((a, b) => {
      const [x, y] = [v(a), v(b)];
      if (x === null) return 1; // nulls last either way
      if (y === null) return -1;
      const c = typeof x === "string" ? x.localeCompare(y as string) : x - (y as number);
      return sort.desc ? -c : c;
    });
  }

  const latest = rows.reduce<Row | null>((m, r) => (!m || r.b.createdBlock > m.b.createdBlock ? r : m), null);
  const topTvl = rows.reduce<Row | null>((m, r) => (!m || r.tvl > m.tvl ? r : m), null);
  const gainer = rows.reduce<Row | null>((m, r) => (r.chg !== null && (!m || r.chg > m.chg!) ? r : m), null);

  return (
    <main className="w-full px-4 pb-24 md:px-6">
      <section className="pt-16 pb-20 text-center md:pt-24 md:pb-28">
        <h1 className="text-[40px] leading-tight font-medium tracking-tight md:text-[60px]">Baskets on Monad</h1>
        <div className="mt-8 md:mt-10">
          <HeroSearch />
        </div>
      </section>

      {all.error && (
        <div className="mx-auto mb-4 max-w-7xl">
          <ErrorBox message={all.error} />
        </div>
      )}

      <section className="mx-auto grid max-w-7xl grid-cols-1 gap-4 pb-16 md:grid-cols-2 md:pb-24 lg:grid-cols-3">
        <Highlight title="Latest Added" row={latest} value={(r) => (r.price ? fmtUsd(r.price) : "—")} />
        <Highlight title="Highest TVL" row={topTvl} value={(r) => fmtUsdCompact(r.tvl)} />
        <Highlight
          title="Biggest Gainer (24H)"
          row={gainer}
          loading={!extras}
          value={(r) => <ChangeText value={r.chg} className="gap-1.5" />}
        />
      </section>

      <section className="mx-auto max-w-7xl space-y-4">
        <div role="tablist" className="flex flex-wrap items-center gap-1">
          {["all", ...(authenticated ? ["mine"] : []), ...cats].map((t) => (
            <button
              key={t}
              role="tab"
              aria-selected={active === t}
              onClick={() => setTab(t)}
              className={cn(
                "rounded-full px-3 py-1 text-[22px] transition-colors",
                active === t ? "bg-secondary text-foreground" : "text-muted-foreground hover:text-foreground",
              )}
            >
              {t === "all" ? "All" : t === "mine" ? "Created by me" : (LABEL[t] ?? tokenBySymbol(t)?.name ?? t)}
            </button>
          ))}
        </div>

        <BasketTable
          rows={shown}
          loading={all.loading}
          sort={sort}
          onSort={(key) =>
            // like tanstack: first click desc (name: asc), second flips, third clears
            setSort((s) =>
              s?.key !== key ? { key, desc: key !== "name" } : s.desc === (key !== "name") ? { key, desc: !s.desc } : null,
            )
          }
          empty={
            active === "mine" ? (
              <>
                You haven&apos;t created a basket yet.{" "}
                <Link href="/create" className="text-foreground underline underline-offset-4">
                  Create one
                </Link>
                .
              </>
            ) : (
              "No baskets yet."
            )
          }
        />
      </section>
    </main>
  );
}

function Highlight({
  title,
  row,
  value,
  loading,
}: {
  title: string;
  row: Row | null;
  value: (r: Row) => React.ReactNode;
  loading?: boolean;
}) {
  const body = (
    <article className="rounded-[22px] bg-secondary/40">
      <p className="px-6 pt-2.5 text-[15px] text-muted-foreground">{title}</p>
      <div className="mt-3 rounded-[22px] border border-white/10 bg-card p-6 transition-[box-shadow,scale] duration-150 hover:ring-2 hover:ring-white/10 active:scale-[0.98]">
        {row && !loading ? (
          <>
            <Sparkline data={row.series} className="h-32 w-full" />
            <div className="mt-1 flex items-center justify-between gap-3">
              <span className="flex min-w-0 items-center gap-2.5">
                <IconStack symbols={row.symbols} size={26} ring="ring-card" />
                <span className="truncate text-[22px] leading-none">{row.b.name}</span>
              </span>
              <span className="tnum shrink-0 text-[18px] leading-none font-medium">{value(row)}</span>
            </div>
          </>
        ) : (
          <>
            <Skeleton className="h-32 w-full" />
            <Skeleton className="mt-1 h-7 w-40" />
          </>
        )}
      </div>
    </article>
  );
  return row ? (
    <Link href={`/baskets/${row.b.address}`} aria-label={`View ${row.b.name}`} className="block">
      {body}
    </Link>
  ) : (
    body
  );
}

type SortKey = "name" | "price" | "chgUsd" | "chg" | "tvl";

const COLS: { h: string; key?: SortKey; c: string }[] = [
  { h: "Basket", key: "name", c: "w-[34%] pl-6 text-left" },
  { h: "Price", key: "price", c: "text-right" },
  { h: "24h ($)", key: "chgUsd", c: "hidden text-right md:table-cell" },
  { h: "24h (%)", key: "chg", c: "text-right" },
  { h: "Last 24h", c: "hidden text-center lg:table-cell" },
  { h: "TVL", key: "tvl", c: "hidden text-right sm:table-cell" },
  { h: "Status", c: "hidden pr-6 text-right md:table-cell" },
];

function BasketTable({
  rows,
  loading,
  sort,
  onSort,
  empty,
}: {
  rows: Row[];
  loading: boolean;
  sort: { key: SortKey; desc: boolean } | null;
  onSort: (key: SortKey) => void;
  empty: React.ReactNode;
}) {
  const router = useRouter();
  return (
    <div className="overflow-hidden rounded-3xl border border-white/10 bg-card shadow-[0_8px_40px_rgba(0,0,0,0.25)]">
      <div className="overflow-x-auto">
        <table className="w-full border-collapse">
          <thead>
            <tr className="border-b border-border bg-secondary/40">
              {COLS.map(({ h, key, c }) => {
                const dir = key && sort?.key === key ? (sort.desc ? "descending" : "ascending") : undefined;
                const Icon = dir === "ascending" ? ChevronUp : dir === "descending" ? ChevronDown : ChevronsUpDown;
                return (
                  <th key={h} aria-sort={dir} className={cn("px-4 py-3 text-[13px] font-medium text-muted-foreground", c)}>
                    {key ? (
                      <button
                        type="button"
                        onClick={() => onSort(key)}
                        className="inline-flex cursor-pointer items-center gap-1 transition-colors hover:text-foreground"
                      >
                        {h}
                        <Icon className={dir ? "size-4" : "size-3.5"} />
                      </button>
                    ) : (
                      h
                    )}
                  </th>
                );
              })}
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {loading ? (
              [0, 1, 2].map((i) => (
                <tr key={i} className="h-16">
                  {COLS.map(({ h, c }) => (
                    <td key={h} className={cn("px-4", c)}>
                      <Skeleton className={cn(h === "Basket" ? "h-8 w-44" : "ml-auto h-4 w-14")} />
                    </td>
                  ))}
                </tr>
              ))
            ) : rows.length > 0 ? (
              rows.map((r) => {
                const open = () => router.push(`/baskets/${r.b.address}`);
                return (
                  <tr
                    key={r.b.address}
                    tabIndex={0}
                    onClick={open}
                    onKeyDown={(e) => e.key === "Enter" && open()}
                    className="h-16 cursor-pointer text-sm transition-colors hover:bg-secondary/40 focus-visible:bg-secondary/40 focus-visible:outline-none"
                  >
                    <td className={cn("px-4", COLS[0].c)}>
                      <div className="flex items-center gap-3">
                        <IconStack symbols={r.symbols} size={26} ring="ring-card" />
                        <span className="truncate text-[15px] font-medium">{r.b.name}</span>
                        <span className="shrink-0 text-muted-foreground">${r.b.symbol}</span>
                      </div>
                    </td>
                    <td className={cn("tnum px-4 font-medium", COLS[1].c)}>{r.price ? fmtUsd(r.price) : "—"}</td>
                    <td className={cn("px-4", COLS[2].c)}>
                      <ChangeText value={r.chgUsd} usd className="justify-end" />
                    </td>
                    <td className={cn("px-4", COLS[3].c)}>
                      <ChangeText value={r.chg} className="justify-end" />
                    </td>
                    <td className={cn("px-4", COLS[4].c)}>
                      <Sparkline data={r.series} area className="mx-auto h-9 w-[140px]" />
                    </td>
                    <td className={cn("tnum px-4 font-medium", COLS[5].c)}>{fmtUsdCompact(r.tvl)}</td>
                    <td className={cn("px-4", COLS[6].c)}>
                      <LiquidityBadge liquid={r.liquid} />
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
