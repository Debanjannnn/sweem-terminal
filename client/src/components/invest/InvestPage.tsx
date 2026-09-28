"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { Search } from "lucide-react";
import { fmtUsdCompact, getBaskets } from "@/lib/api";
import { useAsync, useSession } from "@/lib/session";
import { ErrorBox, Picker } from "@/components/ui";
import { BasketTable } from "./BasketTable";
import { FeaturedCarousel } from "./FeaturedCarousel";
import { useBasketExtras, useMarkets } from "./extras";

type Scope = "all" | "mine";

export function InvestPage() {
  const { authenticated, address } = useSession();
  const [scope, setScope] = useState<Scope>("all");
  const [search, setSearch] = useState("");
  const mine = scope === "mine" && authenticated && address;

  const all = useAsync("baskets", () => getBaskets());
  const own = useAsync(mine ? `baskets:${address}` : null, () => getBaskets(address));
  const list = mine ? own : all;
  const markets = useMarkets();
  const extras = useBasketExtras(all.data);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return (list.data ?? []).filter(
      (b) => q === "" || b.name.toLowerCase().includes(q) || b.symbol.toLowerCase().includes(q),
    );
  }, [list.data, search]);

  const featured = useMemo(
    () => [...(all.data ?? [])].sort((a, b) => Number(b.nav?.totalNav ?? 0) - Number(a.nav?.totalNav ?? 0)).slice(0, 8),
    [all.data],
  );
  const tvl = (all.data ?? []).reduce((s, b) => s + Number(b.nav?.totalNav ?? 0), 0);

  return (
    <div className="w-full space-y-8 px-4 py-10">
      <div className="w-full rounded-2xl border border-border bg-gradient-to-b from-card to-background p-6 sm:p-10">
        <h1 className="text-3xl font-bold tracking-tight sm:text-4xl">Baskets on Monad</h1>
        <p className="mt-4 max-w-lg text-base text-muted-foreground">
          Each basket is an ERC-20 holding a fixed-weight mix of tokens. Buy with USDC and every slice fills on a Kuru
          order book in the same transaction.
        </p>
        <div className="flex flex-wrap items-center gap-6 pt-8 sm:gap-8">
          <Stat value={all.data ? String(all.data.length) : "—"} label="Baskets" />
          <div className="h-10 w-px bg-border" />
          <Stat value={all.data ? fmtUsdCompact(tvl) : "—"} label="Total value locked" />
          <div className="h-10 w-px bg-border" />
          <Stat value={markets.data ? String(markets.data.length) : "—"} label="Kuru markets" />
        </div>
      </div>

      {(all.error || markets.error) && <ErrorBox message={all.error ?? markets.error ?? ""} />}

      {(all.loading || featured.length > 0) && (
        <FeaturedCarousel baskets={featured} markets={markets.data} extras={extras} loading={all.loading} />
      )}

      <div className="flex flex-wrap items-center gap-3">
        {authenticated && (
          <Picker<Scope>
            ariaLabel="Filter by creator"
            value={scope}
            onChange={setScope}
            options={[
              { value: "all", label: "All baskets" },
              { value: "mine", label: "Created by me" },
            ]}
          />
        )}
        <label className="flex w-full items-center gap-2 rounded-full border border-border bg-card px-4 py-2 sm:ml-auto sm:w-auto">
          <Search className="size-4 shrink-0 text-muted-foreground" />
          <span className="sr-only">Search baskets</span>
          <input
            type="text"
            placeholder="Search name or symbol"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground sm:w-48"
          />
        </label>
      </div>

      {own.error && <ErrorBox message={own.error} />}

      <BasketTable
        baskets={filtered}
        markets={markets.data}
        extras={extras}
        loading={list.loading}
        empty={
          (list.data ?? []).length === 0 ? (
            <>
              {mine ? "You haven't created a basket yet." : "No baskets yet."}{" "}
              <Link href="/create" className="text-foreground underline underline-offset-4">
                Create one
              </Link>
              .
            </>
          ) : (
            "No basket matches this search."
          )
        }
      />
    </div>
  );
}

function Stat({ value, label }: { value: string; label: string }) {
  return (
    <div>
      <p className="tnum text-2xl font-bold">{value}</p>
      <p className="text-sm text-muted-foreground">{label}</p>
    </div>
  );
}
