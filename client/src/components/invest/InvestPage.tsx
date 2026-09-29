"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useMemo, useState } from "react";
import { getBaskets } from "@/lib/api";
import { useAsync, useSession } from "@/lib/session";
import { ErrorBox } from "@/components/ui";
import { cn } from "@/lib/utils";
import { BasketTable } from "./BasketTable";
import { useBasketExtras, useMarkets } from "./extras";

type Scope = "all" | "mine";

export function InvestPage() {
  const { authenticated, address } = useSession();
  const [scope, setScope] = useState<Scope>("all");
  const search = useSearchParams().get("q") ?? "";
  const mine = scope === "mine" && authenticated && address;

  const all = useAsync("baskets", () => getBaskets());
  const own = useAsync(mine ? `baskets:${address}` : null, () => getBaskets(address));
  const list = mine ? own : all;
  const markets = useMarkets();
  const extras = useBasketExtras(all.data);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return (list.data ?? [])
      .filter((b) => q === "" || b.name.toLowerCase().includes(q) || b.symbol.toLowerCase().includes(q))
      .sort((a, b) => Number(b.nav?.totalNav ?? 0) - Number(a.nav?.totalNav ?? 0));
  }, [list.data, search]);

  return (
    <main className="w-full space-y-10 px-4 pb-24 md:px-6">
      <section className="mx-auto max-w-3xl pt-20 text-center">
        <h1 className="text-balance text-[40px] leading-[1.05] font-medium tracking-tight md:text-[54px]">
          Tradable token baskets on Monad
        </h1>
        <p className="mx-auto mt-5 max-w-xl text-pretty text-lg text-muted-foreground">
          Own a whole portfolio in one trade. Every basket fills on Kuru order books, and you can put it on autopilot
          with recurring buys and stop-losses.
        </p>
      </section>

      {(all.error || markets.error || own.error) && <ErrorBox message={all.error ?? markets.error ?? own.error ?? ""} />}

      <section className="space-y-4">
        <div role="tablist" className="flex items-center gap-1">
          {(["all", "mine"] as const).map((s) =>
            s === "mine" && !authenticated ? null : (
              <button
                key={s}
                role="tab"
                aria-selected={scope === s}
                onClick={() => setScope(s)}
                className={cn(
                  "rounded-full px-4 py-1.5 text-xl font-medium transition-colors",
                  scope === s ? "bg-secondary text-foreground" : "text-muted-foreground hover:text-foreground",
                )}
              >
                {s === "all" ? "All baskets" : "Created by me"}
              </button>
            ),
          )}
          {search && <span className="ml-auto text-sm text-muted-foreground">Matching “{search}”</span>}
        </div>

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
      </section>
    </main>
  );
}
