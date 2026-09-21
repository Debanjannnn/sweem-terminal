"use client";

import { useMemo, useState } from "react";
import { Bell } from "lucide-react";
import { Dropdown } from "@/components/ui/Dropdown";
import { Skeleton } from "@/components/ui/Skeleton";
import { formatSize, formatUsd } from "@/lib/format";
import { useMarkets, useMarks, usePortfolio } from "@/lib/query";
import { marketIndex, positionRows } from "@/lib/rows";
import { useSession } from "@/lib/session";
import { SearchPill } from "./SearchPill";
import { WalletMenu } from "./WalletMenu";

const CURRENCIES = [
  { value: "USD", label: "USD" },
  { value: "MON", label: "MON" },
] as const;

export function TopBar() {
  const [query, setQuery] = useState("");
  const [currency, setCurrency] = useState<"USD" | "MON">("USD");
  const { authenticated } = useSession();
  const portfolio = usePortfolio();
  const markets = useMarkets();

  // MON display needs a MON price, which only a listed MON market can give
  const monMarket = (markets.data ?? []).find(
    (m) => m.base.toUpperCase() === "MON" && m.kind === "spot",
  );
  const monMark = useMarks(currency === "MON" && monMarket ? [monMarket.id] : []);
  const monPrice = Number(monMark.data?.marks[0]?.price ?? NaN);

  const value = useMemo(() => {
    if (!portfolio.data) return undefined;
    const rows = positionRows(portfolio.data.positions, marketIndex(markets.data ?? []));
    const priced = rows.filter((r) => r.markPrice !== undefined);
    if (!priced.length) return null;
    return priced.reduce((sum, r) => sum + r.size * r.markPrice!, 0);
  }, [portfolio.data, markets.data]);

  return (
    <header className="flex h-16 shrink-0 items-center gap-4 border-b border-border px-6">
      <span className="text-lg font-semibold tracking-tight">sweem</span>
      <SearchPill value={query} onChange={setQuery} className="w-[420px] max-w-lg" />

      <div className="ml-auto flex items-center gap-3">
        <button
          type="button"
          aria-label="Notifications"
          className="rounded-full p-2.5 text-muted-foreground transition-colors hover:text-foreground"
        >
          <Bell className="size-5" />
        </button>

        <div className="flex h-9 items-center rounded-full bg-secondary/70 px-4">
          {!authenticated ? (
            <span className="text-sm text-muted-foreground">Not connected</span>
          ) : portfolio.isPending ? (
            <Skeleton className="h-4 w-24" />
          ) : (
            <span className="tnum text-sm">
              {value == null
                ? "—"
                : currency === "USD"
                  ? formatUsd(value)
                  : Number.isFinite(monPrice) && monPrice > 0
                    ? `${formatSize(value / monPrice, 2)} MON`
                    : "—"}
            </span>
          )}
        </div>

        <Dropdown
          value={currency}
          onChange={setCurrency}
          options={CURRENCIES}
          ariaLabel="Display currency"
        />

        <WalletMenu />
      </div>
    </header>
  );
}
