import {
  fromWad,
  txTier,
  type Execution,
  type Market,
  type Order,
  type PortfolioPosition,
} from "./api";
import type { Position } from "@/components/table/PositionCard";

// same id normalization the backend uses: "mon-usdc", "MON/USDC" and "MONUSDC" match
const normId = (id: string) => id.toUpperCase().replace(/[^A-Z0-9]/g, "");

export const marketIndex = (markets: Market[]) =>
  new Map(markets.map((m) => [normId(m.id), m]));

const scaled = (raw: string, decimals: number) => Number(raw) / 10 ** decimals;

const STATUS: Record<string, Position["status"]> = {
  pending: "pending",
  submitted: "pending",
  failed: "cancelled",
  filled: "open",
};

export function orderRows(
  orders: Order[],
  executions: Execution[],
  markets: Map<string, Market>,
): Position[] {
  const byKey = new Map(executions.map((e) => [e.intent_key, e]));
  return orders.map((o) => {
    const m = markets.get(normId(o.market_id));
    // an order's size is what it spends: quote tokens on a buy, base on a sell
    const decimals = m ? (o.side === "buy" ? m.quoteDecimals : m.baseDecimals) : 18;
    return {
      id: o.intent_key,
      symbol: m?.symbol ?? o.market_id,
      side: o.side === "buy" ? "long" : "short",
      status: STATUS[o.state] ?? "open",
      size: scaled(o.size, decimals),
      tx: txTier(byKey.get(o.intent_key)),
      note: byKey.get(o.intent_key)?.error,
    };
  });
}

export function positionRows(
  positions: PortfolioPosition[],
  markets: Map<string, Market>,
): Position[] {
  return positions.map((p) => {
    const m = markets.get(normId(p.market_id));
    const long = p.side !== "short";
    // indexer sizes are chain WAD; a local row is our own intent's spend units
    const decimals =
      p.source === "indexer" || !m ? 18 : long ? m.quoteDecimals : m.baseDecimals;
    const size = scaled(p.size, decimals);
    const entry = p.entry_price ? fromWad(p.entry_price) : undefined;
    // marks come off /v1/marks already decimal, and are absent when unpriced
    const mark = p.mark ? Number(p.mark) : undefined;
    const priced = entry !== undefined && mark !== undefined && entry !== 0;
    return {
      id: `${p.market_id}:${p.source}`,
      symbol: m?.symbol ?? p.market_id,
      side: long ? "long" : "short",
      status: "open" as const,
      size,
      entryPrice: entry,
      markPrice: mark,
      pnl: priced ? (mark - entry) * size * (long ? 1 : -1) : undefined,
      pnlPct: priced ? (mark / entry - 1) * 100 * (long ? 1 : -1) : undefined,
      note: p.reconciled ? undefined : p.reconcile_reason,
    };
  });
}
