import { deviation } from "./index-math.ts";

// short-window manipulation is the known attack on a pushed index, so this refuses
// the write rather than logging a warning. pure: the publisher does the i/o.
export interface BreakerInput {
  marketId: string;
  next: bigint;
  // null on first publication
  last: bigint | null;
  // WAD, inclusive: exactly this publishes
  maxDeviation: bigint;
  lastPublishedAt: number | null;
  heartbeatMs: number;
  now: number;
}

export interface BreakerTripped {
  idempotencyKey: string;
  emittedAt: number;
  feedId: string;
  deviation: string;
  trippedAt: number;
}

export type BreakerDecision =
  | { publish: true; reason: "first" | "moved" | "heartbeat"; deviation: bigint }
  | { publish: false; reason: "tripped" | "unchanged"; deviation: bigint; event?: BreakerTripped };

export function evaluate(input: BreakerInput): BreakerDecision {
  const { marketId, next, last, maxDeviation, lastPublishedAt, heartbeatMs, now } = input;

  if (last === null) return { publish: true, reason: "first", deviation: 0n };

  const dev = deviation(next, last);

  if (dev > maxDeviation) {
    return {
      publish: false,
      reason: "tripped",
      deviation: dev,
      event: {
        idempotencyKey: `oracle:breaker:${marketId}:${now}`,
        emittedAt: now,
        feedId: marketId,
        deviation: dev.toString(),
        trippedAt: now,
      },
    };
  }

  if (next === last) {
    const stale = lastPublishedAt === null || now - lastPublishedAt >= heartbeatMs;
    return stale
      ? { publish: true, reason: "heartbeat", deviation: dev }
      : { publish: false, reason: "unchanged", deviation: dev };
  }

  return { publish: true, reason: "moved", deviation: dev };
}
