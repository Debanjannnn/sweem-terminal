import { toUnits, type Market } from "./api";

// every kuru precision is a power of ten, so market-data ships them as decimals:
// pricePrecision = 10^priceDecimals, sizePrecision = 10^sizeDecimals. these are
// NOT token decimals and a wrong one silently sends a wrong order size.

const pow = (n: number) => 10n ** BigInt(n);

// a market order is a swap: size is raw atoms of the token going in
export const marketSize = (m: Market, side: "buy" | "sell", human: string) =>
  toUnits(human, side === "buy" ? m.quoteDecimals : m.baseDecimals);

// a deposit moves real tokens, so it is raw atoms of that token
export const depositAmount = (m: Market, token: "base" | "quote", human: string) =>
  toUnits(human, token === "base" ? m.baseDecimals : m.quoteDecimals);

// a limit price is the raw uint32 tick integer
export const limitPrice = (m: Market, human: string) => toUnits(human, m.priceDecimals);

// a limit quantity is book units (size precision), not token atoms
export const limitQuantity = (m: Market, human: string) => toUnits(human, m.sizeDecimals);

// rawBaseAtoms = bookQuantity × baseSizeMultiplier, and the multiplier is
// 10^(baseDecimals - sizeDecimals) on every listed market. a fractional one means
// the market is malformed, and then the atom count is unknowable.
export function rawBaseAtoms(m: Market, quantity: string): string | null {
  const e = m.baseDecimals - m.sizeDecimals;
  return e < 0 ? null : (BigInt(quantity) * pow(e)).toString();
}

// the chain floors twice: into price precision, then into quote atoms
export const quoteNotional = (m: Market, price: string, quantity: string) =>
  (
    ((BigInt(price) * BigInt(quantity)) / pow(m.sizeDecimals) * pow(m.quoteDecimals)) /
    pow(m.priceDecimals)
  ).toString();

export const isZero = (raw: string) => /^0*$/.test(raw);

const display = (raw: string, decimals: number) => Number(raw) / 10 ** decimals;

// an off-tick price reverts and a rounded-away leg is not an order
export function limitProblem(m: Market, price: string, quantity: string): string | null {
  if (isZero(price))
    return `This market prices in ${m.priceDecimals} decimals, so that price rounds to zero — name a larger one.`;
  if (isZero(quantity))
    return `This market sizes in ${m.sizeDecimals} decimals, so that quantity rounds to zero — name a larger one.`;
  const tick = BigInt(m.tickSize || "1");
  if (tick > 1n && BigInt(price) % tick !== 0n)
    return `This market only accepts prices in steps of ${display(m.tickSize, m.priceDecimals)} ${m.quote}, so ${display(price, m.priceDecimals)} is off-tick.`;
  return null;
}
