export const WALLET_URL =
  process.env.NEXT_PUBLIC_WALLET_URL ?? "http://localhost:8080";
export const MARKET_URL =
  process.env.NEXT_PUBLIC_MARKET_DATA_URL ?? "http://localhost:8081";
export const MARKET_WS_URL =
  process.env.NEXT_PUBLIC_MARKET_DATA_WS_URL ?? "ws://localhost:8082";

export const CHAIN_ID = process.env.NEXT_PUBLIC_CHAIN_ID === "143" ? 143 : 10143;

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export const errorMessage = (e: unknown) =>
  e instanceof Error ? e.message : String(e);

type Envelope<T> = { data?: T; error?: { code: string; message: string } };

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  let res: Response;
  try {
    res = await fetch(url, {
      ...init,
      headers: { "Content-Type": "application/json", ...init?.headers },
    });
  } catch (e) {
    throw new ApiError(0, "network", `${url} could not be reached: ${errorMessage(e)}.`);
  }
  let body: Envelope<T> | null = null;
  try {
    body = (await res.json()) as Envelope<T>;
  } catch {
    body = null;
  }
  if (!res.ok || body?.error) {
    const err = body?.error;
    throw new ApiError(
      res.status,
      err?.code ?? "http_error",
      err?.message ?? `${url} answered ${res.status} with no message.`,
    );
  }
  return body?.data as T;
}

export const walletFetch = <T,>(path: string, token: string, init?: RequestInit) =>
  request<T>(WALLET_URL + path, {
    ...init,
    headers: { Authorization: `Bearer ${token}`, ...init?.headers },
  });

export const marketFetch = <T,>(path: string, init?: RequestInit) =>
  request<T>(MARKET_URL + path, init);

// every price, size and volume on the wire is a WAD integer string
export const WAD_DECIMALS = 18;

export function fromWad(s: string | undefined | null, decimals = WAD_DECIMALS): number {
  if (!s) return NaN;
  const n = Number(s);
  return Number.isFinite(n) ? n / 10 ** decimals : NaN;
}

// decimal string -> integer base units, no float rounding on the way out
export function toUnits(value: string, decimals: number): string {
  const [whole = "0", frac = ""] = value.trim().split(".");
  const padded = (frac + "0".repeat(decimals)).slice(0, decimals);
  const digits = `${whole.replace(/^0+(?=\d)/, "")}${padded}`.replace(/^0+(?=\d)/, "");
  return digits === "" ? "0" : digits;
}

export const toWad = (value: string) => toUnits(value, WAD_DECIMALS);

// wad integer string -> integer in another token's decimals
export function wadToUnits(wad: string, decimals: number): string {
  if (decimals >= WAD_DECIMALS) return wad + "0".repeat(decimals - WAD_DECIMALS);
  const cut = WAD_DECIMALS - decimals;
  const out = wad.slice(0, Math.max(0, wad.length - cut)).replace(/^0+(?=\d)/, "");
  return out === "" ? "0" : out;
}

export type Market = {
  id: string;
  venue: string;
  kind: string;
  symbol: string;
  base: string;
  quote: string;
  baseDecimals: number;
  quoteDecimals: number;
  priceDecimals: number;
  sizeDecimals: number;
  tickSize: string;
  minSize: string;
  minIsQuoteNotional: boolean;
  takerFeeMicros: number;
  makerFeeMicros: number;
  status: string;
  address: string;
  mark?: string;
  last?: string;
};

export type Mark = { market_id: string; price: string; ts: number };
export type Level = { price: string; size: string };
export type Book = {
  marketId: string;
  venue: string;
  bids: Level[];
  asks: Level[];
  ts: number;
};
export type Candle = {
  marketId: string;
  resolution: number;
  openTime: number;
  open: string;
  high: string;
  low: string;
  close: string;
  volume: string;
};
export type Tick = {
  marketId: string;
  venue: string;
  price: string;
  size: string;
  side: string;
  ts: number;
};
export type IndexFrame = {
  marketId: string;
  value: string;
  roundId?: string;
  publishedAt?: number;
};
export type Quote = {
  marketId: string;
  venue: string;
  side: string;
  filled: string;
  notional: string;
  avgPrice: string;
  impactBps: number;
  partial: boolean;
};

export type Me = {
  id: string;
  wallet_address: string;
  privy_wallet_id?: string;
  created_at: string;
  first_funded_at?: string;
};

export type Order = {
  intent_key: string;
  user_id: string;
  market_id: string;
  venue: string;
  side: string;
  order_type: string;
  size: string;
  limit_price?: string;
  max_slippage_bps: number;
  chain_id: number;
  state: string;
  created_at: string;
};

export type Execution = {
  intent_key: string;
  tx_hash?: string;
  confirmation: string;
  error?: string;
  submitted_at: string;
  settled_at?: string;
};

export type PortfolioPosition = {
  market_id: string;
  side?: string;
  size: string;
  entry_price?: string;
  collateral?: string;
  // omitted when market-data could not price it; never zero
  mark?: string;
  source: string;
  reconciled: boolean;
  reconcile_reason?: string;
  updated_at?: number;
};

export type Portfolio = {
  user_id: string;
  wallet_address: string;
  positions: PortfolioPosition[];
  degraded: string[];
  updated_at: string;
};

export type SliceState = "pending" | "dispatched" | "filled" | "failed" | "skipped";

export type Slice = {
  schedule_id: string;
  slice_index: number;
  due_at: string;
  amount: string;
  state: SliceState;
  intent_key?: string;
  skip_reason?: string;
  generation: number;
};

export type Schedule = {
  id: string;
  user_id: string;
  kind: "twap" | "dca" | "grid";
  market_id: string;
  venue: string;
  side: string;
  total: string;
  slice_count: number;
  window_seconds: number;
  cadence_seconds: number;
  levels?: unknown;
  state: string;
  idempotency_key: string;
  created_at: string;
  slices?: Slice[];
};

export type Delegation = {
  user_id: string;
  signer_id: string;
  policy_id?: string;
  venues: string[];
  markets: string[];
  max_notional_per_slice: string;
  max_notional_total: string;
  expires_at: string;
  revoked_at?: string;
  live: boolean;
};

export type OrderResponse = {
  intent_key: string;
  state: string;
  tx_hash?: string;
  error?: string;
  replayed?: boolean;
};

// Monad tiers: latest is Proposed and can still change, safe is Voted, finalized is irreversible.
export const txTier = (e: Execution | undefined) => {
  if (!e) return undefined;
  if (e.error) return "failed" as const;
  switch (e.confirmation) {
    case "finalized":
      return "final" as const;
    case "safe":
      return "safe" as const;
    default:
      return "seen" as const;
  }
};
