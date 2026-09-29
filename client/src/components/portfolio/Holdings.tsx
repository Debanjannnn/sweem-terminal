"use client";

import Link from "next/link";
import { useState } from "react";
import { ArrowDown, ArrowUp, ChevronDown, ExternalLink, Fuel, Search } from "lucide-react";
import { fmtUsd } from "@/lib/api";
import { USDC_DECIMALS } from "@/lib/config";
import { fmtUnits } from "@/lib/tokens";
import type { Basket } from "@/lib/types";
import { Panel, Skeleton, useDismissable } from "@/components/ui";
import { TokenIcon } from "@/components/TokenIcon";
import { IconStack } from "@/components/invest/BasketCard";
import { cn } from "@/lib/utils";

export type AssetClass = "Basket" | "Crypto" | "Stablecoin";

export type Row = {
  key: string;
  symbol: string;
  name: string;
  cls: AssetClass;
  icons: string[];
  href?: string;
  price: number | null;
  balance: string;
  value: number | null;
  note?: React.ReactNode;
};

const CHIP: Record<AssetClass, string> = {
  Basket: "border-primary/30 bg-primary/10 text-primary",
  Crypto: "border-chart-2/30 bg-chart-2/10 text-chart-2",
  Stablecoin: "border-border bg-secondary/60 text-muted-foreground",
};

const SELECT = "rounded-full bg-secondary/70 px-3 py-2 text-sm text-foreground outline-none transition-colors hover:bg-secondary";
const CLASSES = [{ value: "all", label: "All asset classes" }, ...(["Basket", "Crypto", "Stablecoin"] as const).map((c) => ({ value: c, label: c }))];

export function Holdings({
  rows,
  loading,
  usdc,
  mon,
  lowGas,
  baskets,
}: {
  rows: Row[];
  loading: boolean;
  usdc: bigint | undefined;
  mon: bigint | undefined;
  lowGas: boolean;
  baskets: Basket[] | null;
}) {
  const [cls, setCls] = useState<"all" | AssetClass>("all");
  const [query, setQuery] = useState("");
  const [desc, setDesc] = useState(true);
  const q = query.trim().toLowerCase();
  const shown = rows
    .filter((r) => (cls === "all" || r.cls === cls) && (!q || `${r.symbol} ${r.name}`.toLowerCase().includes(q)))
    .sort((a, b) => ((a.value ?? -1) - (b.value ?? -1)) * (desc ? -1 : 1));

  return (
    <Panel className="p-5 sm:p-6">
      <div className="flex flex-wrap items-center gap-3">
        <h2 className="mr-auto text-lg font-medium">My Holdings</h2>
        <select aria-label="Asset class" value={cls} onChange={(e) => setCls(e.target.value as typeof cls)} className={SELECT}>
          {CLASSES.map((c) => (
            <option key={c.value} value={c.value}>
              {c.label}
            </option>
          ))}
        </select>
        <select aria-label="Network" className={SELECT}>
          <option>Monad testnet</option>
        </select>
        <label className="flex w-full items-center gap-2 rounded-full bg-secondary/70 px-4 py-2 text-sm sm:w-56">
          <Search className="size-4 shrink-0 text-muted-foreground" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search asset"
            aria-label="Search asset"
            className="w-full bg-transparent outline-none placeholder:text-muted-foreground"
          />
        </label>
      </div>

      <div className="mt-5 grid gap-4 sm:grid-cols-2">
        <div className="flex items-center gap-3 rounded-xl border border-primary/20 bg-primary/5 p-4">
          <TokenIcon symbol="USDC" size={36} />
          <div className="min-w-0 flex-1">
            <div className="text-xs text-primary">USDC</div>
            {usdc === undefined ? (
              <Skeleton className="mt-1 h-6 w-20" />
            ) : (
              <div className="tnum text-lg font-medium">${fmtUnits(usdc, USDC_DECIMALS, 2)}</div>
            )}
          </div>
          <InvestMenu baskets={baskets} />
        </div>
        <div
          className={cn(
            "flex items-center gap-3 rounded-xl border p-4",
            lowGas ? "border-warning/30 bg-warning/10" : "border-border bg-secondary/30",
          )}
        >
          <TokenIcon symbol="MON" size={36} />
          <div className="min-w-0 flex-1">
            <div className={cn("text-xs", lowGas ? "text-warning" : "text-muted-foreground")}>MON · gas</div>
            {mon === undefined ? (
              <Skeleton className="mt-1 h-6 w-20" />
            ) : (
              <div className="tnum text-lg font-medium">{fmtUnits(mon, 18, 4)}</div>
            )}
          </div>
          {lowGas && (
            <a
              href="https://faucet.monad.xyz"
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-1 rounded-lg px-2 py-1 text-xs text-warning underline-offset-4 hover:underline"
            >
              <Fuel className="size-3.5" /> Low gas · Get MON <ExternalLink className="size-3" />
            </a>
          )}
        </div>
      </div>

      <div className="mt-4 overflow-x-auto">
        <table className="w-full min-w-[640px] text-sm">
          <thead className="text-left text-xs text-muted-foreground">
            <tr className="border-b border-border">
              <th className="py-3 pl-3 font-normal">Token</th>
              <th className="px-3 py-3 font-normal">Asset class</th>
              <th className="px-3 py-3 font-normal">Price ($)</th>
              <th className="px-3 py-3 font-normal">Balance</th>
              <th className="py-3 pr-3 font-normal">
                <button onClick={() => setDesc((d) => !d)} className="inline-flex items-center gap-1 hover:text-foreground">
                  Value ($) {desc ? <ArrowDown className="size-3" /> : <ArrowUp className="size-3" />}
                </button>
              </th>
            </tr>
          </thead>
          <tbody className="tnum divide-y divide-border">
            {loading ? (
              [0, 1, 2].map((i) => (
                <tr key={i}>
                  <td className="py-4 pl-3" colSpan={5}>
                    <Skeleton className="h-8 w-full" />
                  </td>
                </tr>
              ))
            ) : shown.length === 0 ? (
              <tr>
                <td colSpan={5} className="py-10 text-center text-sm text-muted-foreground">
                  {rows.length === 0 ? (
                    <>
                      Nothing here yet.{" "}
                      <Link href="/" className="text-foreground underline underline-offset-4 hover:no-underline">
                        Browse baskets
                      </Link>
                    </>
                  ) : (
                    "No assets match."
                  )}
                </td>
              </tr>
            ) : (
              shown.map((r) => (
                <tr key={r.key} className="transition-colors hover:bg-secondary/30">
                  <td className="py-3 pl-3">
                    <div className="flex items-center gap-3">
                      {r.icons.length > 1 ? (
                        <IconStack symbols={r.icons} size={24} ring="ring-card" />
                      ) : (
                        <TokenIcon symbol={r.icons[0] ?? r.symbol} size={28} />
                      )}
                      <div className="min-w-0">
                        {r.href ? (
                          <Link href={r.href} className="font-medium hover:underline">
                            {r.symbol}
                          </Link>
                        ) : (
                          <div className="font-medium">{r.symbol}</div>
                        )}
                        <div className="truncate text-xs text-muted-foreground">{r.name}</div>
                      </div>
                    </div>
                  </td>
                  <td className="px-3 py-3">
                    <span className={cn("rounded-md border px-2 py-0.5 text-[11px]", CHIP[r.cls])}>{r.cls}</span>
                  </td>
                  <td className="px-3 py-3">{r.price === null ? "—" : fmtUsd(r.price)}</td>
                  <td className="px-3 py-3">{r.balance}</td>
                  <td className="py-3 pr-3">
                    {r.value === null ? "—" : fmtUsd(r.value)}
                    {r.note}
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </Panel>
  );
}

function InvestMenu({ baskets }: { baskets: Basket[] | null }) {
  const { open, setOpen, ref } = useDismissable();
  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className="inline-flex items-center gap-1.5 rounded-lg bg-primary px-3 py-1.5 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
      >
        Invest <ChevronDown className="size-4" />
      </button>
      {open && (
        <ul className="absolute right-0 z-30 mt-2 max-h-72 w-60 overflow-y-auto rounded-xl border border-border bg-popover p-1 shadow-xl">
          {!baskets ? (
            <li className="p-2">
              <Skeleton className="h-6 w-full" />
            </li>
          ) : baskets.length === 0 ? (
            <li className="px-3 py-2 text-sm text-muted-foreground">No baskets yet.</li>
          ) : (
            baskets.map((b) => (
              <li key={b.address}>
                <Link
                  href={`/baskets/${b.address}`}
                  onClick={() => setOpen(false)}
                  className="flex items-center gap-2 rounded-lg px-3 py-2 text-sm transition-colors hover:bg-secondary/70"
                >
                  <span className="truncate">{b.name}</span>
                  <span className="ml-auto text-xs text-muted-foreground">{b.symbol}</span>
                </Link>
              </li>
            ))
          )}
        </ul>
      )}
    </div>
  );
}
