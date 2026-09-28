import { ApiError, getMarkets, getNavHistory, getQuote } from "@/lib/api";
import { useAsync } from "@/lib/session";
import { baseToken } from "@/lib/tokens";
import type { Basket, Market } from "@/lib/types";

// liquid: false = mint quote unavailable (a component book has no asks), null = unknown
export type Extras = { change: number | null; liquid: boolean | null };

export function change24h(hist: { navPerShare: string }[]): number | null {
  if (hist.length < 2) return null;
  const first = Number(hist[0].navPerShare);
  const last = Number(hist[hist.length - 1].navPerShare);
  return first > 0 && last > 0 ? ((last - first) / first) * 100 : null;
}

async function extrasOf(address: string): Promise<Extras> {
  const [hist, quote] = await Promise.allSettled([getNavHistory(address), getQuote(address, "mint", 1_000_000n)]);
  return {
    change: hist.status === "fulfilled" ? change24h(hist.value) : null,
    liquid:
      quote.status === "fulfilled"
        ? true
        : quote.reason instanceof ApiError && quote.reason.message === "unavailable"
          ? false
          : null,
  };
}

// ponytail: one history + quote request per basket, batch endpoint if the list grows past ~50
export function useBasketExtras(baskets: Basket[] | null) {
  const key = baskets ? `extras:${baskets.map((b) => b.address).join()}` : null;
  const res = useAsync(key, async () => {
    const list = await Promise.all((baskets ?? []).map((b) => extrasOf(b.address)));
    return new Map((baskets ?? []).map((b, i) => [b.address, list[i]]));
  });
  return res.data;
}

export const useMarkets = () => useAsync("markets", getMarkets);

export const symbolOf = (market: string, markets: Market[] | null) => baseToken(market, markets)?.symbol ?? "…";
