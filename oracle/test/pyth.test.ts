import { describe, expect, it } from "bun:test";
import type { PublicClient } from "viem";
import { pythSource, toWad } from "../src/sources/pyth.ts";

const ETH_USD = "0xff61491a931112ddf1bd8147cd1b641375f79f5825126d665480874634fd0ace";

const client = (price: bigint, expo: number, publishTimeSec: number) =>
  ({
    readContract: async () => ({ price, conf: 44_876_625n, expo, publishTime: BigInt(publishTimeSec) }),
  }) as unknown as Pick<PublicClient, "readContract">;

describe("expo scaling", () => {
  it("scales by 10**expo, not a hardcoded 8 decimals", () => {
    // live monad-mainnet read: ETH/USD 268432191609 at expo -8 == $2684.32191609
    expect(toWad(268_432_191_609n, -8)).toBe(2_684_321_916_090_000_000_000n);
    expect(toWad(1n, -18)).toBe(1n);
    expect(toWad(123n, -5)).toBe(123n * 10n ** 13n);
    expect(toWad(7n, 0)).toBe(7n * 10n ** 18n);
  });

  it("divides when a feed carries more precision than WAD", () => {
    expect(toWad(1_500n, -21)).toBe(1n);
  });
});

describe("pyth source", () => {
  const now = () => Math.floor(Date.now() / 1000);

  it("accepts a fresh sample and reports the source's publishTime", async () => {
    const ts = now() - 6;
    const s = await pythSource(client(268_432_191_609n, -8, ts), "0x1", ETH_USD, 60_000).read();
    expect(s.price).toBe(2_684_321_916_090_000_000_000n);
    expect(s.sourceTs).toBe(ts * 1000);
  });

  it("rejects a stale sample: getPriceUnsafe does not check, so we must", () => {
    // monad testnet's pyth was 68h behind when this was written
    const old = now() - 68 * 3600;
    expect(pythSource(client(268_432_191_609n, -8, old), "0x1", ETH_USD, 60_000).read()).rejects.toThrow(/stale/);
  });

  it("rejects a non-positive price", () => {
    expect(pythSource(client(0n, -8, now()), "0x1", ETH_USD, 60_000).read()).rejects.toThrow(/non-positive/);
  });
});
