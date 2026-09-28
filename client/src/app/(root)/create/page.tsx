"use client";

import { getMarkets } from "@/lib/api";
import { useAsync } from "@/lib/session";
import { baseToken } from "@/lib/tokens";
import { ErrorBox, Skeleton } from "@/components/ui";

export default function CreatePage() {
  const markets = useAsync("markets", getMarkets);
  return (
    <div className="w-full space-y-3 px-4 py-10">
      {markets.error && <ErrorBox message={markets.error} />}
      {markets.loading && <Skeleton className="h-16 w-full" />}
      {markets.data?.map((m) => (
        <p key={m.market}>{baseToken(m.market, markets.data)?.symbol}/USDC</p>
      ))}
    </div>
  );
}
