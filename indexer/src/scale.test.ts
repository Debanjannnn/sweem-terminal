import { describe, expect, it } from "vitest";
import {
  abs, divWad, mulWad, SECONDS_PER_YEAR, SHARE_DECIMALS, sharesToWad,
  toApr, toWad, usdcToWad, USDC_DECIMALS, WAD,
} from "./scale.js";

describe("USDC 6 -> WAD 18", () => {
  it.each([
    [0n, 0n],
    [1n, 10n ** 12n],
    [1_000_000n, WAD],
    [1_150_000n, (115n * WAD) / 100n],
    [123_456_789n, 123_456_789n * 10n ** 12n],
  ])("usdcToWad(%s)", (input, expected) => {
    expect(usdcToWad(input)).toBe(expected);
  });

  // a left shift, so the round trip is lossless
  it("loses nothing on the round trip", () => {
    for (const a of [1n, 7n, 999_999n, 42_000_000_000n]) {
      expect(usdcToWad(a) / 10n ** 12n).toBe(a);
    }
  });

  it("is the identity for values already at 18 decimals", () => {
    expect(sharesToWad(WAD)).toBe(WAD);
    expect(toWad(12345n, 18)).toBe(12345n);
    expect(SHARE_DECIMALS).toBe(18);
    expect(USDC_DECIMALS).toBe(6);
  });

  it("widens any decimals, not just 6", () => {
    expect(toWad(5n, 0)).toBe(5n * WAD);
    expect(toWad(5n, 8)).toBe(5n * 10n ** 10n);
  });
});

describe("fixed point", () => {
  it("mulWad and divWad invert each other", () => {
    expect(mulWad(3n * WAD, WAD / 2n)).toBe((15n * WAD) / 10n);
    expect(divWad(3n * WAD, WAD / 2n)).toBe(6n * WAD);
  });

  // genesis: no shares yet, so the caller wants 0, not a division fault
  it("divWad returns 0 rather than throwing on a zero denominator", () => {
    expect(divWad(WAD, 0n)).toBe(0n);
  });

  it("truncates toward zero on both signs", () => {
    expect(mulWad(-1n, WAD / 2n)).toBe(0n);
    expect(mulWad(1n, WAD / 2n)).toBe(0n);
  });

  it("annualizes a per-second rate so 1e18 is 100%/yr", () => {
    expect(toApr(WAD / SECONDS_PER_YEAR)).toBe(999999999975888000n);
    expect(toApr(0n)).toBe(0n);
    expect(SECONDS_PER_YEAR).toBe(31_536_000n);
  });

  it.each([
    [5n, 5n],
    [-5n, 5n],
    [0n, 0n],
  ])("abs(%s)", (input, expected) => {
    expect(abs(input)).toBe(expected);
  });
});
