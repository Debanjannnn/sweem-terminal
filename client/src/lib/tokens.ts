import { formatUnits, parseUnits, zeroAddress } from "viem";
import type { Market } from "./types";

export type Token = {
  address: string;
  symbol: string;
  name: string;
  decimals: number;
  icon?: string;
  color?: string;
};

const list: Token[] = [
  { address: "0xee0722ead54f1b4fe97be399be43bc0226a6f97e", symbol: "USDC", name: "USD Coin", decimals: 6, icon: "/tokens/usdc.png" },
  { address: "0x8b6c5fafef85b030bb1e71ae7ac085cc2380aaf8", symbol: "WETH", name: "Wrapped Ether", decimals: 18, icon: "/tokens/eth.svg" },
  { address: "0xef2a20a161ac9ed1117d721336226b6399f15b4d", symbol: "cbBTC", name: "Coinbase wrapped BTC", decimals: 8, icon: "/tokens/cbbtc.png" },
  { address: "0xee1dce135a9ab598bca8cf3a28bdef6892100740", symbol: "XAUT", name: "Tether Gold", decimals: 6, color: "#D4AF37", icon: "/tokens/xaut.png" },
  { address: zeroAddress, symbol: "MON", name: "Monad", decimals: 18, color: "#836EF9", icon: "/tokens/mon.png" },
];

// keyed by lowercase address
export const TOKENS: Record<string, Token> = Object.fromEntries(list.map((t) => [t.address, t]));

export const tokenBySymbol = (symbol: string) => list.find((t) => t.symbol === symbol);

export const tokenOf = (address: string): Token =>
  TOKENS[address.toLowerCase()] ?? {
    address: address.toLowerCase(),
    symbol: `${address.slice(0, 6)}…`,
    name: address,
    decimals: 18,
  };

// base token of a market, via /markets
export const baseToken = (market: string, markets: readonly Market[] | null | undefined): Token | undefined => {
  const m = markets?.find((x) => x.market === market.toLowerCase());
  return m && { ...tokenOf(m.base), decimals: m.baseDecimals };
};

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
