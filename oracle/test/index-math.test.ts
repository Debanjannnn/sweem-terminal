import { describe, expect, it } from "bun:test";
import {
  SECONDS_PER_YEAR,
  annualizedSigma,
  deviation,
  ewmaVariance,
  initialEwma,
  logReturn,
  samplesPerYear,
  stepEwma,
} from "../src/index-math.ts";
import { UNIT, mulDiv18, ud } from "../src/prb.ts";

const LAMBDA = 940_000_000_000_000_000n; // 0.94

describe("sample cadence", () => {
  it("annualizes on a 365-day year", () => {
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
