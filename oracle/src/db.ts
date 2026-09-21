import postgres from "postgres";
import type { Sample } from "./sources/source.ts";

export type Sql = ReturnType<typeof connect>;

export const connect = (url: string) => postgres(url, { max: 4 });

export async function migrate(sql: Sql): Promise<void> {
  const file = Bun.file(new URL("../migrations/0001_oracle.sql", import.meta.url));
  await sql.unsafe(await file.text()).simple();
}

export interface PublicationRow {
  marketId: string;
  kind: "vol" | "power";
  value: bigint;
  previous: bigint | null;
  deviation: bigint;
  status: "published" | "rejected" | "failed";
  reason: string;
  roundId: bigint | null;
  txHash: string | null;
}

// idempotent on (symbol, source, source_ts): a redelivered sample is a no-op
export async function insertSample(sql: Sql, source: string, symbol: string, s: Sample): Promise<void> {
  await sql`
    INSERT INTO oracle.price_samples (source, symbol, price, source_ts, round_id)
    VALUES (${source}, ${symbol}, ${s.price.toString()}, to_timestamp(${s.sourceTs}::double precision / 1000),
            ${s.roundId?.toString() ?? null})
    ON CONFLICT (symbol, source, source_ts) DO NOTHING`;
}

// so a restart does not reset the ewma to zero and fake a vol spike
export async function recentPrices(sql: Sql, symbol: string, limit: number): Promise<bigint[]> {
  const rows = await sql<{ price: string }[]>`
    SELECT price FROM oracle.price_samples WHERE symbol = ${symbol} ORDER BY source_ts DESC LIMIT ${limit}`;
  return rows.map((r) => BigInt(r.price)).reverse();
}

export async function recordPublication(sql: Sql, r: PublicationRow): Promise<void> {
  await sql`
    INSERT INTO oracle.index_publications
      (market_id, kind, value, previous, deviation, status, reason, round_id, tx_hash)
    VALUES (${r.marketId}, ${r.kind}, ${r.value.toString()}, ${r.previous?.toString() ?? null},
            ${r.deviation.toString()}, ${r.status}, ${r.reason}, ${r.roundId?.toString() ?? null}, ${r.txHash})`;
}

// breaker baseline: the last value confirmed on-chain, not the last computed
export async function lastPublished(sql: Sql, marketId: string): Promise<{ value: bigint; at: number } | null> {
  const [row] = await sql<{ value: string; created_at: Date }[]>`
    SELECT value, created_at FROM oracle.index_publications
    WHERE market_id = ${marketId} AND status = 'published'
    ORDER BY created_at DESC LIMIT 1`;
  return row ? { value: BigInt(row.value), at: row.created_at.getTime() } : null;
}
