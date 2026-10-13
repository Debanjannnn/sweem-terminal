"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useState } from "react";
import { ChevronDown, ChevronUp, ChevronsUpDown, Plus } from "lucide-react";
import { motion } from "motion/react";
import { fmtUsd, fmtUsdCompact, getBaskets, shortHash } from "@/lib/api";
import { useTickers, type Ticker } from "@/lib/hooks";
import { useAsync } from "@/lib/session";
import { MARKETS } from "@/lib/tokens";
import { cn } from "@/lib/utils";
import type { Basket } from "@/lib/types";
import { ErrorBox, Notice, Skeleton } from "@/components/ui";
import { HeroSearch } from "@/components/BasketSearch";
import { Sparkline } from "@/components/Sparkline";
import { EASE, Flash, Reveal, TabPill } from "@/components/motion";
import { ChangeText, IconStack } from "@/components/invest/BasketCard";
import { TokenIcon } from "@/components/TokenIcon";
import { basketIndex } from "@/components/basket/index";

type Row = {
  key: string;
  href: string;
  name: string;
  sub: string;
  icons: string[];
  series: number[];
  price: number | null;
  chgUsd: number | null;
  chg: number | null;
  vol: number | null;
};

export const basketRow = (b: Basket, tickers: Record<string, Ticker>): Row => {
  const series = basketIndex(b.legs.map((l) => ({ weightBps: l.weightBps, candles: tickers[l.market]?.candles ?? [] }))).map((p) => p.v);
  const chg = series.length > 1 ? series.at(-1)! - series[0] : null;
  return {
    key: b.id,
    href: `/baskets/${b.id}`,
    name: b.name,
    sub: b.legs.map((l) => `${l.market} ${l.weightBps / 100}%`).join(" · "),
    icons: b.legs.map((l) => l.market),
    series,
    price: null,
    chgUsd: null,
    chg,
    vol: null,
  };
};

export function HomePage() {
  const params = useSearchParams();
  const [tab, setTab] = useState<"tokens" | "baskets">(params.get("tab") === "baskets" ? "baskets" : "tokens");
  const { tickers, loading, error } = useTickers();
  const baskets = useAsync("baskets", () => getBaskets());
  const [sort, setSort] = useState<{ key: SortKey; desc: boolean } | null>(null);

  const tokenRows: Row[] = MARKETS.map((m) => {
    const t = tickers[m.symbol];
    return {
      key: m.symbol,
      href: `/trade/${m.symbol}`,
      name: m.token.name,
      sub: `$${m.symbol}`,
      icons: [m.symbol],
      series: t.series,
      price: t.price,
      chgUsd: t.chgUsd,
      chg: t.chg,
      vol: t.candles.length ? t.vol : null,
    };
  });
  const basketRows = (baskets.data ?? []).map((b) => basketRow(b, tickers));
  const rows = [...(tab === "tokens" ? tokenRows : basketRows)];
  if (sort) {
    const v = (r: Row) => (sort.key === "name" ? r.name : r[sort.key]);
    rows.sort((a, b) => {
      const [x, y] = [v(a), v(b)];
      if (x === null) return 1;
      if (y === null) return -1;
      const c = typeof x === "string" ? x.localeCompare(y as string) : x - (y as number);
      return sort.desc ? -c : c;
    });
  }

  const top = (list: Row[], f: (r: Row) => number | null) => list.reduce<Row | null>((m, r) => (f(r) !== null && (!m || f(r)! > f(m)!) ? r : m), null);
  const mon = tokenRows.find((r) => r.key === "MON") ?? null;
  const gainer = top(tokenRows, (r) => r.chg);
  const topBasket = top(basketRows, (r) => r.chg);

  return (
    <main className="w-full px-4 pb-24 md:px-6">
      <section className="pb-20 pt-16 text-center md:pb-28 md:pt-24">
        <h1 className="text-[40px] font-medium leading-tight tracking-tight md:text-[60px]">Trade on Monad</h1>
        <div className="mt-8 md:mt-10">
          <HeroSearch />
        </div>
      </section>

      {(error || baskets.error) && (
        <div className="mx-auto mb-4 max-w-7xl">
          <ErrorBox message={`Market data unavailable: ${error ?? baskets.error}`} />
        </div>
      )}
      {!loading && MARKETS.every((m) => tickers[m.symbol].price === null) && (
        <div className="mx-auto mb-4 max-w-7xl">
          <Notice>Mainnet market feed is offline. Prices, charts and quotes appear once it&apos;s back.</Notice>
        </div>
      )}

      <section className="mx-auto grid max-w-7xl grid-cols-1 gap-4 pb-16 md:grid-cols-2 md:pb-24 lg:grid-cols-3">
        <Reveal>
          <Highlight title="Monad" row={mon} loading={loading} value={(r) => (r.price ? <Flash value={r.price}>{fmtUsd(r.price)}</Flash> : "-")} />
        </Reveal>
        <Reveal delay={0.05}>
          <Highlight title="Biggest Gainer (24H)" row={gainer} loading={loading} value={(r) => <ChangeText value={r.chg} className="gap-1.5" />} />
        </Reveal>
        <Reveal delay={0.1}>
          <Highlight title="Top Basket (24H)" row={topBasket} loading={loading || baskets.loading} value={(r) => <ChangeText value={r.chg} className="gap-1.5" />} />
        </Reveal>
      </section>

      <section className="mx-auto max-w-7xl space-y-4">
        <div className="flex flex-wrap items-center gap-1">
          <div role="tablist" className="flex flex-wrap items-center gap-1">
            {(["tokens", "baskets"] as const).map((t) => (
              <button
                key={t}
                role="tab"
                aria-selected={tab === t}
                onClick={() => (setTab(t), setSort(null))}
                className={cn("relative rounded-full px-3 py-1 text-[22px] transition-colors", tab === t ? "text-foreground" : "text-muted-foreground hover:text-foreground")}
              >
                {tab === t && <TabPill id="home-cat" className="rounded-full bg-secondary" />}
                <span className="relative">{t === "tokens" ? "Tokens" : "Baskets"}</span>
              </button>
            ))}
          </div>
          {tab === "baskets" && (
            <Link href="/baskets/new" className="ml-auto inline-flex items-center gap-1.5 rounded-full bg-secondary px-3.5 py-2 text-sm font-medium hover:bg-accent">
              <Plus className="size-4" /> New basket
            </Link>
          )}
        </div>

        <Table
          rows={rows}
          baskets={tab === "baskets"}
          loading={tab === "tokens" ? loading : baskets.loading || loading}
          sort={sort}
          onSort={(key) =>
            setSort((s) => (s?.key !== key ? { key, desc: key !== "name" } : s.desc === (key !== "name") ? { key, desc: !s.desc } : null))
          }
          owners={Object.fromEntries((baskets.data ?? []).map((b) => [b.id, b.owner]))}
        />
      </section>
    </main>
  );
}

function Highlight({ title, row, value, loading }: { title: string; row: Row | null; value: (r: Row) => React.ReactNode; loading?: boolean }) {
  const body = (
    <article className="rounded-[22px] bg-secondary/40">
      <p className="px-6 pt-2.5 text-[15px] text-muted-foreground">{title}</p>
      <div className="mt-3 rounded-[22px] border border-white/10 bg-card p-6 transition-[box-shadow,scale,translate,border-color] duration-200 ease-out hover:-translate-y-0.5 hover:border-white/15 hover:ring-2 hover:ring-white/10 active:scale-[0.98]">
        {row && !loading ? (
          <>
            <Sparkline data={row.series} className="h-32 w-full" />
            <div className="mt-1 flex items-center justify-between gap-3">
              <span className="flex min-w-0 items-center gap-2.5">
                <IconStack symbols={row.icons} size={26} ring="ring-card" />
                <span className="truncate text-[22px] leading-none">{row.name}</span>
              </span>
              <span className="tnum shrink-0 text-[18px] font-medium leading-none">{value(row)}</span>
            </div>
          </>
        ) : loading ? (
          <>
            <Skeleton className="h-32 w-full" />
            <Skeleton className="mt-1 h-7 w-40" />
          </>
        ) : (
          <p className="grid h-40 place-items-center text-sm text-muted-foreground">Nothing yet.</p>
        )}
      </div>
    </article>
  );
  return row ? (
    <Link href={row.href} aria-label={`View ${row.name}`} className="block">
      {body}
    </Link>
  ) : (
    body
  );
}

type SortKey = "name" | "price" | "chgUsd" | "chg" | "vol";

function Table({
  rows,
  baskets,
  loading,
  sort,
  onSort,
  owners,
}: {
  rows: Row[];
  baskets: boolean;
  loading: boolean;
  sort: { key: SortKey; desc: boolean } | null;
  onSort: (key: SortKey) => void;
  owners: Record<string, string>;
}) {
  const router = useRouter();
  const cols: { h: string; key?: SortKey; c: string }[] = baskets
    ? [
        { h: "Basket", key: "name", c: "w-[40%] pl-6 text-left" },
        { h: "24h (%)", key: "chg", c: "text-right" },
        { h: "Last 24h", c: "hidden text-center lg:table-cell" },
        { h: "Owner", c: "hidden pr-6 text-right md:table-cell" },
      ]
    : [
        { h: "Token", key: "name", c: "w-[30%] pl-6 text-left" },
        { h: "Price", key: "price", c: "text-right" },
        { h: "24h ($)", key: "chgUsd", c: "hidden text-right md:table-cell" },
        { h: "24h (%)", key: "chg", c: "text-right" },
        { h: "Last 24h", c: "hidden text-center lg:table-cell" },
        { h: "24h Volume", key: "vol", c: "hidden pr-6 text-right sm:table-cell" },
      ];
  return (
    <div className="overflow-hidden rounded-3xl border border-white/10 bg-card shadow-[0_8px_40px_rgba(0,0,0,0.25)]">
      <div className="overflow-x-auto">
        <table className="w-full border-collapse">
          <thead>
            <tr className="border-b border-border bg-secondary/40">
              {cols.map(({ h, key, c }) => {
                const dir = key && sort?.key === key ? (sort.desc ? "descending" : "ascending") : undefined;
                const Icon = dir === "ascending" ? ChevronUp : dir === "descending" ? ChevronDown : ChevronsUpDown;
                return (
                  <th key={h} aria-sort={dir} className={cn("px-4 py-3 text-[13px] font-medium text-muted-foreground", c)}>
                    {key ? (
                      <button type="button" onClick={() => onSort(key)} className="inline-flex cursor-pointer items-center gap-1 transition-colors hover:text-foreground">
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
            {loading && rows.every((r) => !r.series.length) ? (
              [0, 1, 2].map((i) => (
                <tr key={i} className="h-16">
                  {cols.map(({ h, c }, j) => (
                    <td key={h} className={cn("px-4", c)}>
                      <Skeleton className={cn(j === 0 ? "h-8 w-44" : "ml-auto h-4 w-14")} />
                    </td>
                  ))}
                </tr>
              ))
            ) : rows.length === 0 ? (
              <tr>
                <td colSpan={cols.length} className="py-10 text-center text-sm text-muted-foreground">
                  No baskets yet.{" "}
                  <Link href="/baskets/new" className="text-foreground underline underline-offset-4">
                    Create one
                  </Link>
                  .
                </td>
              </tr>
            ) : (
              rows.map((r, i) => {
                const open = () => router.push(r.href);
                return (
                  <motion.tr
                    key={r.key}
                    initial={{ opacity: 0, y: 4 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.25, delay: Math.min(i, 10) * 0.03, ease: EASE }}
                    tabIndex={0}
                    onClick={open}
                    onKeyDown={(e) => e.key === "Enter" && open()}
                    className="h-16 cursor-pointer text-sm transition-colors duration-150 hover:bg-secondary/40 focus-visible:bg-secondary/40 focus-visible:outline-none"
                  >
                    <td className={cn("px-4", cols[0].c)}>
                      <div className="flex items-center gap-3">
                        {r.icons.length > 1 ? <IconStack symbols={r.icons} size={26} ring="ring-card" /> : <TokenIcon symbol={r.icons[0]} size={26} />}
                        <span className="truncate text-[15px] font-medium">{r.name}</span>
                        <span className="truncate text-muted-foreground">{r.sub}</span>
                      </div>
                    </td>
                    {baskets ? (
                      <>
                        <td className={cn("px-4", cols[1].c)}>
                          <ChangeText value={r.chg} className="justify-end" />
                        </td>
                        <td className={cn("px-4", cols[2].c)}>
                          <Sparkline data={r.series} area className="mx-auto h-9 w-[140px]" />
                        </td>
                        <td className={cn("tnum px-4 text-muted-foreground", cols[3].c)}>{owners[r.key] ? shortHash(owners[r.key]) : "-"}</td>
                      </>
                    ) : (
                      <>
                        <td className={cn("tnum px-4 font-medium", cols[1].c)}>{r.price ? <Flash value={r.price}>{fmtUsd(r.price)}</Flash> : "-"}</td>
                        <td className={cn("px-4", cols[2].c)}>
                          <ChangeText value={r.chgUsd} usd className="justify-end" />
                        </td>
                        <td className={cn("px-4", cols[3].c)}>
                          <ChangeText value={r.chg} className="justify-end" />
                        </td>
                        <td className={cn("px-4", cols[4].c)}>
                          <Sparkline data={r.series} area className="mx-auto h-9 w-[140px]" />
                        </td>
                        <td className={cn("tnum px-4 font-medium", cols[5].c)}>{r.vol !== null ? fmtUsdCompact(r.vol) : "-"}</td>
                      </>
                    )}
                  </motion.tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
