import { getAccessToken } from "@privy-io/react-auth";
import { API_URL, HISTORY_URL, MARKET_DATA_URL } from "./config";
import type {
  Accepted,
  Backtest,
  Book,
  Candle,
  Earnings,
  Execution,
  FundingDay,
  Holdings,
  Interval,
  Me,
  Order,
  PerplMarkets,
  PerplRisk,
  Position,
  PrepareInput,
  Prepared,
  Quote,
  Rates,
  Schedule,
  ScheduleInput,
  Strategy,
  StrategyInput,
} from "./types";

// status 0 = network error; message = backend snake_code when it sent one; detail = planner/history detail.
export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
    readonly detail?: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  let res: Response;
  try {
    res = await fetch(url, init);
  } catch (e) {
    throw new ApiError(0, `cannot reach ${url} (${e instanceof Error ? e.message : "network error"})`);
  }
  const text = await res.text();
  let body: unknown = null;
  try {
    body = text ? JSON.parse(text) : null;
  } catch {
    body = text;
  }
  if (!res.ok) {
    const { error: code, detail } = (body ?? {}) as { error?: unknown; detail?: unknown };
    throw new ApiError(
      res.status,
      typeof code === "string" ? code : `request failed (${res.status})`,
      typeof detail === "string" ? detail : undefined,
    );
  }
  return body as T;
}

async function authed<T>(path: string, method = "GET", body?: unknown): Promise<T> {
  const token = await getAccessToken();
  if (!token) throw new ApiError(401, "unauthorized");
  return request<T>(API_URL + path, {
    method,
    headers: {
      Authorization: `Bearer ${token}`,
      ...(body === undefined ? {} : { "Content-Type": "application/json" }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

const q = (params: Record<string, string | number | undefined>) => {
  const s = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== "") s.set(k, String(v));
  const out = s.toString();
  return out ? `?${out}` : "";
};

const json = (body: unknown): RequestInit => ({
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify(body),
});

// api — public
export const getStrategies = (creator?: string) => request<Strategy[]>(`${API_URL}/strategies${q({ creator })}`);
// authed when logged in so the creator can see their private strategy
export const getStrategy = async (id: string) => {
  const token = await getAccessToken().catch(() => null);
  return request<Strategy>(`${API_URL}/strategies/${id}`, token ? { headers: { Authorization: `Bearer ${token}` } } : undefined);
};

// api — /me (Privy bearer)
export const getMe = () => authed<Me>("/me");
export const putDelegation = (walletId: string, walletAddress: string) =>
  authed<Me["delegation"]>("/me/delegation", "POST", { walletId, walletAddress });
export const deleteDelegation = () => authed<{ revoked: boolean }>("/me/delegation", "DELETE");
// client-signed: unsigned txs for the user's own wallet (no delegation), then hashes back for on-chain verification
export const prepareTrade = (b: PrepareInput & { wallet: string }) => authed<Prepared>("/me/trade/prepare", "POST", b);
export const recordTrade = (prepareId: string, txHashes: string[]) => authed<Execution>("/me/trade/record", "POST", { prepareId, txHashes });
export const placeOrder = (b: { market: string; side: "buy" | "sell"; price: string; size: string; postOnly?: boolean; fund?: boolean }) =>
  authed<Accepted>("/me/orders", "POST", b);
export const getOrders = () => authed<Order[]>("/me/orders");
export const cancelOrder = (id: string) => authed<Accepted>(`/me/orders/${id}`, "DELETE");
export const getHoldings = () => authed<Holdings>("/me/holdings");
export const createStrategy = (s: StrategyInput) => authed<Strategy>("/strategies", "POST", s);
export const getMyStrategies = () => authed<Strategy[]>("/me/strategies");
export const deployStrategy = (id: string, amountUsdc: string, slippageBps?: number) =>
  authed<Accepted>(`/me/strategies/${id}/deploy`, "POST", { amountUsdc, slippageBps });
// bps 1..10000; < 10000 = partial sell, position stays open
export const withdrawPosition = (id: string, bps = 10000) => authed<Accepted>(`/me/positions/${id}/withdraw`, "POST", { bps });
export const getEarnings = () => authed<Earnings>("/me/earnings");
export const getPositionPerpl = (id: string) => authed<PerplRisk[]>(`/me/positions/${id}/perpl`);
export const getPerplMarkets = () => request<PerplMarkets>(`${API_URL}/perpl/markets`);
export const MARGIN_TOKENS = ["USDC", "MON", "WETH", "cbBTC", "XAUt0"] as const;
export type MarginToken = (typeof MARGIN_TOKENS)[number];
// amount human decimal
export const depositMargin = (token: MarginToken, amount: string) => authed<Accepted>("/me/margin/deposit", "POST", { token, amount });
export const withdrawMargin = (token: MarginToken, amount: string) => authed<Accepted>("/me/margin/withdraw", "POST", { token, amount });
export const getPositions = () => authed<Position[]>("/me/positions");
export const getExecutions = () => authed<Execution[]>("/me/executions");
export const getExecution = (id: string) => authed<Execution>(`/me/executions/${id}`);
export const getSchedules = () => authed<Schedule[]>("/me/schedules");
export const createSchedule = (input: ScheduleInput) => authed<Schedule>("/me/schedules", "POST", input);
export const deleteSchedule = (id: string) => authed<unknown>(`/me/schedules/${id}`, "DELETE");

// market-data (orderbook addresses). 503 "stale" when the feed is down.
export const getBook = (orderbook: string) => request<Book>(`${MARKET_DATA_URL}/books/${orderbook}`);
// amount raw atoms: buy = USDC, sell = base
export const getQuote = (orderbook: string, side: "buy" | "sell", amount: bigint, slippageBps = 50) =>
  request<Quote>(`${MARKET_DATA_URL}/quote/${orderbook}${q({ side, amount: amount.toString(), slippageBps })}`);
export const getCandles = (orderbook: string, interval: Interval, from?: number, to?: number) =>
  request<Candle[]>(`${MARKET_DATA_URL}/candles/${orderbook}${q({ interval, from, to })}`);
export const getRates = () => request<Rates>(`${MARKET_DATA_URL}/rates`);

// history
export type BacktestLeg = { kind: string; venue?: string; perpId?: number; weight: string };
export type BacktestSleeve = { asset: string; amount: string; legs: BacktestLeg[]; rules?: Record<string, unknown>; weight: string };
// single token: asset + legs (amount in asset units); multi: sleeves (amount in USD, sleeve weights sum 1)
export type BacktestBody = {
  asset?: string;
  amount: string;
  from?: string;
  to?: string;
  legs?: BacktestLeg[];
  sleeves?: BacktestSleeve[];
  rules?: Record<string, unknown>;
};
export const runBacktest = (body: BacktestBody) => request<Backtest>(`${HISTORY_URL}/backtest`, json(body));
// daily Perpl funding (from YYYY-MM-DD to yesterday)
export const getFunding = (perpId: number, from: string) =>
  request<{ perpId: number; data: FundingDay[] }>(`${HISTORY_URL}/series/funding${q({ perpId, from })}`).then((r) => r.data);

// display formatters (numbers already in display units)
export const fmtUsdCompact = (n: number) =>
  n.toLocaleString("en-US", { style: "currency", currency: "USD", notation: "compact", maximumFractionDigits: 1 });

// sub-cent values keep 2 significant digits (MON moves) instead of rounding to $0.00
export const fmtUsd = (n: number) =>
  n !== 0 && Math.abs(n) < 0.01
    ? n.toLocaleString("en-US", { style: "currency", currency: "USD", maximumSignificantDigits: 2 })
    : n.toLocaleString("en-US", { style: "currency", currency: "USD", maximumFractionDigits: Math.abs(n) < 1 ? 4 : 2 });

export const fmtNum = (n: number, maxFrac = 4) => n.toLocaleString("en-US", { maximumFractionDigits: maxFrac });

export const fmtBps = (bps: number) => `${(bps / 100).toFixed(2)}%`;

export const shortHash = (h: string) => (h.length > 14 ? `${h.slice(0, 8)}…${h.slice(-6)}` : h);

export const fmtTime = (iso: string | number) => {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? String(iso) : d.toLocaleString();
};

export const errMsg = (e: unknown): string =>
  (e as { shortMessage?: string })?.shortMessage ?? (e instanceof Error ? e.message : String(e));

const COPY: Record<string, string> = {
  delegation_inactive: "Enable automation first. sweem needs your delegated signer to run this.",
  below_min_size: "Amount is below the market's minimum order size.",
  exceeds_safe_size: "Too large for current book depth.",
  insufficient_liquidity: "Order book can't fill this size right now.",
  cap_exceeded: "A single step exceeds the per-trade safety cap.",
  unsupported_source: "One of the sources isn't supported for this asset.",
  treasury_unset: "Fees are not configured yet (treasury unset).",
  too_many_steps: "Plan is too long (over 32 steps).",
  quote_unavailable: "Market data is down or stale, try again shortly.",
  history_unavailable: "Backtest service unavailable.",
  rpc_unavailable: "Monad RPC unavailable, try again shortly.",
  executor_unavailable: "Executor unreachable; nothing was sent.",
  position_not_open: "Position isn't open.",
  order_not_open: "Order isn't live on Kuru yet.",
  stale: "Price feed is stale, try again shortly.",
  unavailable: "No liquidity on this side right now.",
  unauthorized: "Session expired, sign in again.",
  invalid_wallet: "Wallet address is invalid.",
  tx_mismatch: "A sent transaction doesn't match the prepared one.",
  tx_pending: "Transaction not mined yet, retrying.",
  quote_expired: "Quote expired before every transaction was sent. Nothing more was sent; review and trade again.",
  not_found: "Not found.",
  no_data: "Not enough history for this setup.",
  invalid_name: "Name is invalid.",
  invalid_legs: "Legs are invalid (1–10 unique markets, weights sum 100%).",
  invalid_sources: "Sources are invalid.",
  invalid_asset: "Asset is invalid.",
};

// user-facing text: known code → copy (+ detail), else raw message
export const errText = (e: unknown): string => {
  const m = errMsg(e);
  const d = e instanceof ApiError && e.detail ? ` (${e.detail})` : "";
  return (COPY[m] ?? m) + d;
};
