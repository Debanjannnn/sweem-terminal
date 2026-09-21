import { describe, expect, it } from "bun:test";
import {
  SECONDS_PER_YEAR,
  annualizedSigma,
  deviation,
  ewmaVariance,
  initialEwma,
  logReturn,
  powerFundingTerm,
  powerIndex,
  samplesPerYear,
  stepEwma,
} from "../src/index-math.ts";
import { UNIT, mulDiv18, ud } from "../src/prb.ts";

const LAMBDA = 940_000_000_000_000_000n; // 0.94

describe("sample cadence", () => {
  it("annualizes on a 365-day year, matching Accrual.sol's constant", () => {
    expect(SECONDS_PER_YEAR).toBe(31_536_000n);
    expect(samplesPerYear(15_000)).toBe(2_102_400n * UNIT);
    expect(samplesPerYear(1000)).toBe(SECONDS_PER_YEAR * UNIT);
  });
});

describe("ewma realized volatility", () => {
  it("converges to the sigma of a series with known per-sample volatility", () => {
    // alternating up/down by exactly exp(s) makes every log return ±s, so var -> s^2
    // and sigma -> s*sqrt(samplesPerYear), computed by a different route than the code.
    const s = UNIT / 1000n;
    const perYear = samplesPerYear(15_000);
    const up = ud.exp(s);
    let price = 1000n * UNIT;
    let state = initialEwma();
    for (let i = 0; i < 4000; i++) {
      price = i % 2 === 0 ? mulDiv18(price, up) : mulDiv18(price, ud.div(UNIT, up));
      state = stepEwma(state, price, LAMBDA);
    }

    const sigma = annualizedSigma(state.variance, perYear);
    const expected = ud.mul(s, ud.sqrt(perYear));
    const err = sigma > expected ? sigma - expected : expected - sigma;
    expect((err * 1_000_000_000n) / expected).toBeLessThan(1_000n);
    expect(state.samples).toBe(4000);
  });

  it("gives exactly zero on a flat series", () => {
    let state = initialEwma();
    for (let i = 0; i < 100; i++) state = stepEwma(state, 1234n * UNIT, LAMBDA);
    expect(state.variance).toBe(0n);
    expect(annualizedSigma(state.variance, samplesPerYear(15_000))).toBe(0n);
  });

  it("holds variance at lambda=1 and discards history at lambda=0", () => {
    expect(ewmaVariance(7n * UNIT, UNIT, UNIT / 2n)).toBe(7n * UNIT);
    expect(ewmaVariance(7n * UNIT, 0n, UNIT / 2n)).toBe(mulDiv18(UNIT / 2n, UNIT / 2n));
  });

  it("rejects a lambda outside [0,1]", () => {
    expect(() => ewmaVariance(0n, UNIT + 1n, 0n)).toThrow(RangeError);
    expect(() => ewmaVariance(0n, -1n, 0n)).toThrow(RangeError);
  });

  it("signs the log return by direction but weights both directions equally", () => {
    const up = logReturn(1010n * UNIT, 1000n * UNIT);
    const down = logReturn(1000n * UNIT, 1010n * UNIT);
    expect(up).toBe(9950330853168072n); // ln(1.01)
    expect(down).toBeLessThan(0n);
    expect(ewmaVariance(0n, 0n, up)).toBe(ewmaVariance(0n, 0n, -up));
  });

  it("seeds on the first sample without inventing a return", () => {
    expect(stepEwma(initialEwma(), 100n * UNIT, LAMBDA)).toEqual({
      variance: 0n,
      lastPrice: 100n * UNIT,
      samples: 1,
    });
  });

  it("rejects a non-positive price", () => {
    expect(() => stepEwma(initialEwma(), 0n, LAMBDA)).toThrow(RangeError);
    expect(() => stepEwma(initialEwma(), -1n, LAMBDA)).toThrow(RangeError);
  });
});

describe("power index", () => {
  const P0 = 1000n * UNIT;

  it("handles integer, fractional and negative k", () => {
    expect(powerIndex(2n * P0, P0, 2n * UNIT)).toBe(4n * UNIT);
    expect(powerIndex(4n * P0, P0, UNIT / 2n)).toBe(2n * UNIT);
    expect(powerIndex(2n * P0, P0, UNIT / 2n)).toBe(1414213562373095048n);
    expect(powerIndex(2n * P0, P0, -(UNIT / 2n))).toBe(707106781186547524n);
  });

  it("is 1 at the anchor for every k, and for k=0 at any price", () => {
    for (const k of [2n * UNIT, UNIT / 2n, -UNIT]) expect(powerIndex(P0, P0, k)).toBe(UNIT);
    expect(powerIndex(7n * P0, P0, 0n)).toBe(UNIT);
  });

  it("guards the domain where the contract reverts", () => {
    expect(() => powerIndex(0n, P0, UNIT)).toThrow(/ZeroPrice/);
    expect(() => powerIndex(P0, 0n, UNIT)).toThrow(/ZeroPrice/);
  });

  it("computes negative k as (P0/P)^|k|, the route PowerIndex.sol takes", () => {
    const k = -(UNIT / 3n);
    expect(powerIndex(3n * P0, P0, k)).toBe(ud.pow(ud.div(P0, 3n * P0), -k));
  });
});

describe("vol -> power funding coupling", () => {
  const sigma = UNIT / 2n; // 50% annualized

  it("is k(k-1)/2 * sigma^2", () => {
    expect(powerFundingTerm(2n * UNIT, sigma)).toBe(mulDiv18(sigma, sigma));
    expect(powerFundingTerm(3n * UNIT, sigma)).toBe(mulDiv18(3n * UNIT, mulDiv18(sigma, sigma)));
  });

  it("is zero for a linear market and for zero vol", () => {
    expect(powerFundingTerm(UNIT, sigma)).toBe(0n);
    expect(powerFundingTerm(2n * UNIT, 0n)).toBe(0n);
  });

  it("pays the other way for 0 < k < 1", () => {
    // k=0.5: 0.5*(-0.5)/2 = -0.125 of sigma^2
    expect(powerFundingTerm(UNIT / 2n, sigma)).toBe(-mulDiv18(UNIT / 8n, mulDiv18(sigma, sigma)));
  });

  it("scales with sigma squared, so the vol index dominates the term", () => {
    expect(powerFundingTerm(2n * UNIT, 2n * sigma)).toBe(4n * powerFundingTerm(2n * UNIT, sigma));
  });
});

describe("deviation", () => {
  it("is symmetric and relative to the baseline", () => {
    expect(deviation(120n * UNIT, 100n * UNIT)).toBe(UNIT / 5n);
    expect(deviation(80n * UNIT, 100n * UNIT)).toBe(UNIT / 5n);
    expect(deviation(100n * UNIT, 100n * UNIT)).toBe(0n);
  });

  it("treats a zero baseline as no deviation rather than dividing by zero", () => {
    expect(deviation(5n * UNIT, 0n)).toBe(0n);
  });
});
