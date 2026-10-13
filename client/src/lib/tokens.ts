import { formatUnits, parseUnits } from "viem";
import { DEPLOYMENTS } from "./config";

export type Token = { address: string; symbol: string; name: string; decimals: number; icon?: string; color?: string };

const META: Record<string, { name: string; icon?: string; color?: string }> = {
  MON: { name: "Monad", icon: "/tokens/mon.png", color: "#836EF9" },
  USDC: { name: "USD Coin", icon: "/tokens/usdc.png" },
  WETH: { name: "Ether", icon: "/tokens/eth.svg" },
  cbBTC: { name: "Bitcoin", icon: "/tokens/cbbtc.png" },
  XAUt0: { name: "Gold", icon: "/tokens/xaut.png", color: "#D4AF37" },
  AUSD: { name: "Agora USD", icon: "/tokens/ausd.png", color: "#3b82f6" },
  USDT0: { name: "Tether USD", icon: "/tokens/usdt0.png", color: "#26A17B" },
  WMON: { name: "Wrapped Monad", icon: "/tokens/mon.png" },
  shMON: { name: "shMonad", icon: "/tokens/shmon.png", color: "#836EF9" },
};

const list: Token[] = Object.entries(DEPLOYMENTS.tokens).map(([symbol, t]) => ({
  symbol,
  address: t.address.toLowerCase(),
  decimals: t.decimals,
  name: META[symbol]?.name ?? symbol,
  icon: META[symbol]?.icon,
  color: META[symbol]?.color,
}));

export const tokenBySymbol = (symbol: string) => list.find((t) => t.symbol === symbol);

// symbol = 143.json key (api market id); token = base, quote = what you pay/receive
export type MarketDef = { symbol: string; orderbook: string; token: Token; quote: Token; minSize: number; sizePrecision: number };

// Kuru markets from 143.json; base-symbol keys are the USDC books, BASE_QUOTE keys the rest. orderbook lowercased (market-data paths/topics use it)
export const MARKETS: MarketDef[] = Object.entries(DEPLOYMENTS.kuru.markets).map(([symbol, m]) => ({
  symbol,
  orderbook: m.orderbook.toLowerCase(),
  token: tokenBySymbol(m.base)!,
  quote: tokenBySymbol(m.quote)!,
  minSize: m.minSize / m.sizePrecision,
  sizePrecision: m.sizePrecision,
}));
export const marketOf = (symbol: string) => MARKETS.find((m) => m.symbol.toLowerCase() === symbol.toLowerCase());
// every book for the same base token (quote selector on the trade page)
export const pairsOf = (m: MarketDef) => MARKETS.filter((x) => x.token.symbol === m.token.symbol);
// one row per base token for lists/search: the first (USDC) book
export const LISTED = MARKETS.filter((m, i) => MARKETS.findIndex((x) => x.token.symbol === m.token.symbol) === i);
export const USDC = tokenBySymbol("USDC")!;

// bigint-safe: rounds down to maxFrac digits, groups thousands
export function fmtUnits(raw: bigint | string, decimals: number, maxFrac = 4): string {
  const s = formatUnits(BigInt(raw), decimals);
  const neg = s.startsWith("-");
  const [int, frac = ""] = (neg ? s.slice(1) : s).split(".");
  const f = frac.slice(0, maxFrac).replace(/0+$/, "");
  return `${neg ? "-" : ""}${int.replace(/\B(?=(\d{3})+(?!\d))/g, ",")}${f ? `.${f}` : ""}`;
}

// user input → raw; null when not a valid non-negative amount
export function parseAmount(input: string, decimals: number): bigint | null {
  const s = input.trim();
  if (!/^\d*\.?\d*$/.test(s) || s === "" || s === ".") return null;
  const [, frac = ""] = s.split(".");
  if (frac.length > decimals) return null;
  return parseUnits(s, decimals);
}
