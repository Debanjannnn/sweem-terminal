import type { NatsConnection } from "nats";
import { type Account, type Address, type Hex, type PublicClient, type WalletClient, keccak256, stringToHex } from "viem";
import { evaluate } from "./breaker.ts";
import type { OracleEnv } from "./config.ts";
import { lastPublished, recordPublication, type Sql } from "./db.ts";
import type { Feed } from "./state.ts";

// IndexOracle rejects any feedId that is not its own, so a misrouted push reverts
export const feedId = (marketId: string): Hex => keccak256(stringToHex(marketId));

const abi = [
  {
    name: "pushIndex",
    type: "function",
    stateMutability: "nonpayable",
    inputs: [
      { name: "feedId", type: "bytes32" },
      { name: "newValue", type: "uint256" },
    ],
    outputs: [{ type: "uint64" }],
  },
] as const;

// ponytail: core nats publish, no jetstream dedup. switch to js.publish with
// msgID=idempotencyKey once a service owns the SWEEM stream.

// internal/shared/finality.FinalityDepth. publications back settlement and a
// receipt is not settlement on monad.
const FINALITY_DEPTH = 5;

export type Outcome =
  | { status: "published"; txHash: Hex; roundId: bigint }
  | { status: "rejected"; reason: "tripped" | "unchanged" }
  | { status: "failed"; error: string };

export class Publisher {
  constructor(
    private readonly env: OracleEnv,
    private readonly client: PublicClient,
    private readonly wallet: WalletClient | null,
    private readonly sql: Sql,
    private readonly nc: NatsConnection | null,
    private readonly oracleFor: (marketId: string) => Address | undefined,
  ) {}

  async publish(feed: Feed): Promise<Outcome> {
    const now = Date.now();
    const last = await lastPublished(this.sql, feed.marketId);

    const decision = evaluate({
      marketId: feed.marketId,
      next: feed.value,
      last: last?.value ?? null,
      maxDeviation: this.env.ORACLE_MAX_DEVIATION,
      lastPublishedAt: last?.at ?? null,
      heartbeatMs: this.env.ORACLE_HEARTBEAT_MS,
      now,
    });

    if (!decision.publish) {
      // an unchanged index is a no-op, not an event: recording it would move the
      // last-published timestamp and the heartbeat would never fire again.
      if (decision.reason === "unchanged") return { status: "rejected", reason: "unchanged" };

      await this.record(feed, last?.value ?? null, decision.deviation, "rejected", decision.reason, null, null);
      if (decision.event) this.nc?.publish("risk.breaker_tripped.v1", JSON.stringify(decision.event));
      return { status: "rejected", reason: "tripped" };
    }

    const previous = last?.value ?? null;
    const oracle = this.oracleFor(feed.marketId);
    if (!oracle || !this.wallet?.account) {
      const error = oracle
        ? "PROTOCOL_SIGNER_KEY unset"
        : `no IndexOracle address configured for ${feed.marketId}`;
      return this.fail(feed, previous, decision.deviation, error);
    }

    try {
      const { request, result } = await this.client.simulateContract({
        address: oracle,
        abi,
        functionName: "pushIndex",
        args: [feedId(feed.marketId), feed.value],
        account: this.wallet.account as Account,
      });
      const txHash = await this.wallet.writeContract(request);
      await this.client.waitForTransactionReceipt({ hash: txHash, confirmations: FINALITY_DEPTH });

      await this.record(feed, previous, decision.deviation, "published", decision.reason, result, txHash);
      this.nc?.publish(
        "oracle.index_published.v1",
        JSON.stringify({
          idempotencyKey: `oracle:${feed.marketId}:${result}`,
          emittedAt: Date.now(),
          marketId: feed.marketId,
          value: feed.value.toString(),
          roundId: result.toString(),
          publishedAt: Date.now(),
        }),
      );
      return { status: "published", txHash, roundId: result };
    } catch (err) {
      return this.fail(feed, previous, decision.deviation, err instanceof Error ? err.message : String(err));
    }
  }

  private async fail(feed: Feed, previous: bigint | null, dev: bigint, error: string): Promise<Outcome> {
    await this.record(feed, previous, dev, "failed", error.slice(0, 500), null, null);
    return { status: "failed", error };
  }

  private record(
    feed: Feed,
    previous: bigint | null,
    deviation: bigint,
    status: "published" | "rejected" | "failed",
    reason: string,
    roundId: bigint | null,
    txHash: Hex | null,
  ) {
    return recordPublication(this.sql, {
      marketId: feed.marketId,
      kind: feed.kind,
      value: feed.value,
      previous,
      deviation,
      status,
      reason,
      roundId,
      txHash,
    });
  }
}
