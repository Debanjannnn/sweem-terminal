import { expect, test } from "bun:test";
import type { Market } from "./api";
import {
  depositAmount,
  limitPrice,
  limitProblem,
  limitQuantity,
  marketSize,
  quoteNotional,
  rawBaseAtoms,
} from "./units";

// both read off chain today: WETHUSDC pricePrecision 100, sizePrecision 1e10,
// CBBTCUSDC pricePrecision 100, sizePrecision 1e8
const weth: Market = {
  id: "0xa9c2",
  venue: "kuru",
  kind: "spot",
  symbol: "WETHUSDC",
  base: "WETH",
  quote: "USDC",
  baseDecimals: 18,
  quoteDecimals: 6,
  priceDecimals: 2,
  sizeDecimals: 10,
  tickSize: "1",
  minSize: "10000000000000000000",
  minIsQuoteNotional: true,
  takerFeeMicros: 70_000,
  makerFeeMicros: 40_000,
  status: "active",
  address: "0xa9c2",
};

const cbbtc: Market = {
  ...weth,
  symbol: "CBBTCUSDC",
  base: "cbBTC",
  baseDecimals: 8,
  sizeDecimals: 8,
  tickSize: "25",
};

test("a market order is sized in raw atoms of the token going in", () => {
  expect(marketSize(weth, "buy", "250.5")).toBe("250500000"); // quote atoms
  expect(marketSize(weth, "sell", "0.033")).toBe("33000000000000000"); // base atoms
  expect(marketSize(cbbtc, "sell", "0.5")).toBe("50000000");
});

test("a deposit is raw atoms of the chosen side of the pair", () => {
  expect(depositAmount(weth, "quote", "12")).toBe("12000000");
  expect(depositAmount(weth, "base", "1.5")).toBe("1500000000000000000");
});

test("a limit price is a tick integer and a quantity is book units", () => {
  expect(limitPrice(weth, "2748.84")).toBe("274884");
  expect(limitQuantity(weth, "0.033")).toBe("330000000");
  expect(limitQuantity(cbbtc, "0.033")).toBe("3300000");
});

test("rawBaseAtoms = bookQuantity × baseSizeMultiplier", () => {
  // 0.033 WETH: 3.3e8 book units × 1e8 = 3.3e16 atoms
  expect(rawBaseAtoms(weth, "330000000")).toBe("33000000000000000");
  // cbBTC's size precision equals its token decimals, so the multiplier is 1
  expect(rawBaseAtoms(cbbtc, "3300000")).toBe("3300000");
  expect(rawBaseAtoms({ ...weth, sizeDecimals: 20 }, "1")).toBeNull();
});

test("quote notional floors twice, matching the chain", () => {
  // 274884 ticks × 3.3e8 book units -> 90.71 USDC
  expect(quoteNotional(weth, "274884", "330000000")).toBe("90710000");
  expect(quoteNotional(weth, "1", "1")).toBe("0");
});

test("limitProblem names the unit that fails, or nothing", () => {
  expect(limitProblem(weth, "274884", "330000000")).toBeNull();
  expect(limitProblem(weth, "0", "330000000")).toContain("rounds to zero");
  expect(limitProblem(weth, "274884", "0")).toContain("rounds to zero");
  // 274884 is not a multiple of a 25-tick market; 274875 is
  expect(limitProblem(cbbtc, "274884", "3300000")).toContain("off-tick");
  expect(limitProblem(cbbtc, "274875", "3300000")).toBeNull();
});
