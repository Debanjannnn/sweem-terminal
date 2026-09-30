"use client";

import { useEffect, useState } from "react";
import { erc20Abi, formatUnits, zeroAddress } from "viem";
import { ApiError, getBook, getCandles, getExecution, getHoldings, getRates } from "./api";
import { DEPLOYMENTS, publicClient } from "./config";
import { useAsync, useSession } from "./session";
import { MARKETS } from "./tokens";
import { subscribe, useTopic } from "./ws";
import type { Book, Candle, Execution, Holdings, Interval, Rates } from "./types";

const H = 3600e3;

export const mid = (b: Book | null | undefined): number | null => {
  const px = [b?.bids[0]?.[0], b?.asks[0]?.[0]].filter(Boolean).map(Number);
  return px.length ? px.reduce((a, c) => a + c) / px.length : null;
};

// snapshot, then live WS frames
export function useBook(orderbook: string | undefined) {
  const snap = useAsync(orderbook ? `book:${orderbook}` : null, () => getBook(orderbook!));
  const live = useTopic<Book>(orderbook ? `book.${orderbook}` : null);
  return { book: live ?? snap.data, stale: !live && !!snap.data?.stale, error: snap.error, loading: snap.loading };
}

export type Ticker = { price: number | null; series: number[]; candles: Candle[]; chg: number | null; chgUsd: number | null; vol: number };

// every Kuru market: 24h hourly candles + live WS mid; price falls back to the last close
export function useTickers() {
  const hist = useAsync("tickers", async () => {
    const res = await Promise.allSettled(MARKETS.map((m) => getCandles(m.orderbook, "1h", Date.now() - 24 * H)));
    return Object.fromEntries(MARKETS.map((m, i) => [m.symbol, res[i].status === "fulfilled" ? res[i].value : []]));
  });
  const [mids, setMids] = useState<Record<string, number | null>>({});
  useEffect(() => {
    const offs = MARKETS.map((m) => subscribe(`book.${m.orderbook}`, (d) => setMids((x) => ({ ...x, [m.symbol]: mid(d as Book) }))));
    return () => offs.forEach((off) => off());
  }, []);
  const out: Record<string, Ticker> = {};
  for (const m of MARKETS) {
    const candles = hist.data?.[m.symbol] ?? [];
    const closes = candles.map((c) => Number(c.c));
    const live = mids[m.symbol] ?? null;
    const price = live ?? closes.at(-1) ?? null;
    const first = Number(candles[0]?.o ?? 0);
    out[m.symbol] = {
      price,
      candles,
      series: live !== null && closes.length ? [...closes, live] : closes,
      chg: price !== null && first > 0 ? ((price - first) / first) * 100 : null,
      chgUsd: price !== null && first > 0 ? price - first : null,
      vol: candles.reduce((s, c) => s + Number(c.q), 0),
    };
  }
  return { tickers: out, loading: hist.loading, error: hist.error };
}

// wallet balances straight from chain (api has no holdings row before the first trade/delegation)
async function chainHoldings(wallet: `0x${string}`): Promise<Holdings> {
  const toks = Object.entries(DEPLOYMENTS.tokens).filter(([, t]) => BigInt(t.address) !== BigInt(0));
  const [mon, erc20] = await Promise.all([
    publicClient.getBalance({ address: wallet }),
    publicClient.multicall({
      allowFailure: false,
      contracts: toks.map(([, t]) => ({ address: t.address as `0x${string}`, abi: erc20Abi, functionName: "balanceOf", args: [wallet] }) as const),
    }),
  ]);
  const row = (symbol: string, address: string, raw: bigint, decimals: number) => ({ symbol, address: address.toLowerCase(), raw: raw.toString(), amount: formatUnits(raw, decimals) });
  return {
    wallet,
    tokens: [row("MON", zeroAddress, mon, 18), ...toks.map(([sym, t], i) => row(sym, t.address, erc20[i], t.decimals))],
    baskets: [],
  };
}

export function useHoldings() {
  const { ready, authenticated, address } = useSession();
  return useAsync(ready && authenticated && address ? `holdings:${address}` : null, () =>
    getHoldings().catch((e) => {
      if (e instanceof ApiError && e.status === 404) return chainHoldings(address!);
      throw e;
    }),
  );
}

// polled every 30s; WS `rates` topic overrides when the socket is up
export function useRates() {
  const snap = useAsync("rates", getRates);
  const live = useTopic<Rates>("rates");
  return { rates: live ?? snap.data, error: snap.error, loading: snap.loading };
}

const DONE = new Set(["confirmed", "failed", "rejected"]);

// polls GET /me/executions/{id} every 2s until it settles
export function useExecution(id: string | null) {
  const [ex, setEx] = useState<{ id: string; data: Execution | null; error: string | null } | null>(null);
  useEffect(() => {
    if (!id) return;
    let live = true;
    let t: ReturnType<typeof setTimeout>;
    const tick = async () => {
      try {
        const data = await getExecution(id);
        if (!live) return;
        setEx({ id, data, error: null });
        if (!DONE.has(data.status)) t = setTimeout(tick, 2000);
      } catch (e) {
        if (!live) return;
        setEx((p) => ({ id, data: p?.id === id ? p.data : null, error: e instanceof Error ? e.message : String(e) }));
        t = setTimeout(tick, 4000);
      }
    };
    void tick();
    return () => {
      live = false;
      clearTimeout(t);
    };
  }, [id]);
  return ex?.id === id ? ex : null;
}

export const settled = (e: Execution | null | undefined) => !!e && DONE.has(e.status);

// candles for a chart range, with the live WS candle merged in by open time
// ms null = full history the feed keeps
export function useCandles(orderbook: string | undefined, iv: Interval, ms: number | null) {
  const hist = useAsync(orderbook ? `candles:${orderbook}:${iv}:${ms}` : null, () => getCandles(orderbook!, iv, ms === null ? undefined : Date.now() - ms));
  const live = useTopic<Candle>(orderbook ? `candles.${orderbook}.${iv}` : null);
  const rows = hist.data ?? [];
  const merged = live && live.interval === iv && rows.length && live.t >= rows[rows.length - 1].t
    ? [...rows.filter((c) => c.t !== live.t), live]
    : rows;
  return { candles: merged, loading: hist.loading, error: hist.error };
}
