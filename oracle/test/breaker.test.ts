import { describe, expect, it } from "bun:test";
import { evaluate } from "../src/breaker.ts";

const WAD = 10n ** 18n;
const base = {
  marketId: "sweem:vol:ETH/USD",
  maxDeviation: WAD / 10n, // 10%
  heartbeatMs: 300_000,
  now: 1_700_000_000_000,
};

describe("deviation breaker", () => {
  it("publishes the first value with no baseline to compare against", () => {
    expect(evaluate({ ...base, next: 5n * WAD, last: null, lastPublishedAt: null })).toEqual({
      publish: true,
      reason: "first",
      deviation: 0n,
    });
  });

  it("publishes a move inside the band", () => {
    const d = evaluate({ ...base, next: 109n * WAD, last: 100n * WAD, lastPublishedAt: base.now - 1000 });
    expect(d).toMatchObject({ publish: true, reason: "moved" });
  });

  it("publishes at exactly the limit", () => {
    const d = evaluate({ ...base, next: 110n * WAD, last: 100n * WAD, lastPublishedAt: base.now - 1000 });
    expect(d.publish).toBe(true);
    expect(d.deviation).toBe(base.maxDeviation);
  });

  it("refuses beyond the limit and emits an auditable event", () => {
    const d = evaluate({ ...base, next: 111n * WAD, last: 100n * WAD, lastPublishedAt: base.now - 1000 });
    expect(d.publish).toBe(false);
    if (d.publish) throw new Error("unreachable");
    expect(d.reason).toBe("tripped");
    expect(d.event).toEqual({
      idempotencyKey: `oracle:breaker:${base.marketId}:${base.now}`,
      emittedAt: base.now,
      feedId: base.marketId,
      deviation: (110_000_000_000_000_000n).toString(),
      trippedAt: base.now,
    });
  });

  it("trips symmetrically on a crash", () => {
    expect(evaluate({ ...base, next: 80n * WAD, last: 100n * WAD, lastPublishedAt: base.now }).publish).toBe(false);
  });

  it("skips an unchanged value until the heartbeat is due", () => {
    const fresh = evaluate({ ...base, next: 100n * WAD, last: 100n * WAD, lastPublishedAt: base.now - 1000 });
    expect(fresh).toMatchObject({ publish: false, reason: "unchanged" });
    const due = evaluate({ ...base, next: 100n * WAD, last: 100n * WAD, lastPublishedAt: base.now - 300_000 });
    expect(due).toMatchObject({ publish: true, reason: "heartbeat" });
  });
});
