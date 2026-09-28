"use client";

import Link from "next/link";
import { getBaskets } from "@/lib/api";
import { useAsync } from "@/lib/session";
import { ErrorBox, Skeleton } from "@/components/ui";

export default function InvestPage() {
  const baskets = useAsync("baskets", () => getBaskets());
  return (
    <div className="w-full space-y-3 px-4 py-10">
      {baskets.error && <ErrorBox message={baskets.error} />}
      {baskets.loading && <Skeleton className="h-16 w-full" />}
      {baskets.data?.map((b) => (
        <Link key={b.address} href={`/baskets/${b.address}`} className="block rounded-xl border border-border bg-card p-4">
          {b.name} <span className="text-muted-foreground">{b.symbol}</span>
        </Link>
      ))}
    </div>
  );
}
