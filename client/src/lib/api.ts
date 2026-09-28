import { getAccessToken } from "@privy-io/react-auth";
import { API_URL, MARKET_DATA_URL } from "./config";
import type {
  Basket,
  Book,
  Execution,
  LiveNav,
  Market,
  Me,
  Nav,
  Position,
  Quote,
  Schedule,
  ScheduleInput,
  Transfer,
} from "./types";

// status 0 = network error; message = backend snake_code when it sent one.
export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
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
    const code = (body as { error?: unknown } | null)?.error;
    throw new ApiError(res.status, typeof code === "string" ? code : `request failed (${res.status})`);
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

// api — public
export const getBaskets = (creator?: string) =>
  request<Basket[]>(`${API_URL}/baskets${q({ creator: creator?.toLowerCase() })}`);
export const getBasket = (address: string) =>
  request<Basket>(`${API_URL}/baskets/${address.toLowerCase()}`);
// default window: last 24h
export const getNavHistory = (address: string, from?: Date, to?: Date) =>
  request<Nav[]>(
    `${API_URL}/baskets/${address.toLowerCase()}/nav/history${q({ from: from?.toISOString(), to: to?.toISOString() })}`,
  );

// api — /me (Privy bearer)
export const getMe = () => authed<Me>("/me");
export const getPositions = () => authed<Position[]>("/me/positions");
export const getHistory = () => authed<Transfer[]>("/me/history");
export const getExecutions = () => authed<Execution[]>("/me/executions");
export const getSchedules = () => authed<Schedule[]>("/me/schedules");
export const createSchedule = (input: ScheduleInput) => authed<Schedule>("/me/schedules", "POST", input);
export const deleteSchedule = (id: string) => authed<{ deactivated: boolean }>(`/me/schedules/${id}`, "DELETE");
export const putDelegation = (walletId: string, walletAddress: string) =>
  authed<Me["delegation"]>("/me/delegation", "POST", { walletId, walletAddress });
export const deleteDelegation = () => authed<{ revoked: boolean }>("/me/delegation", "DELETE");

// market-data
export const getMarkets = () => request<Market[]>(`${MARKET_DATA_URL}/markets`);
export const getLiveNav = (basket: string) =>
  request<LiveNav>(`${MARKET_DATA_URL}/baskets/${basket.toLowerCase()}/nav`);
export const getBook = (market: string) => request<Book>(`${MARKET_DATA_URL}/books/${market.toLowerCase()}`);
// amount raw: USDC 6dp for mint, shares 18dp for redeem. 503 "stale" when feed is down.
export const getQuote = (basket: string, side: "mint" | "redeem", amount: bigint, slippageBps = 50) =>
  request<Quote>(
    `${MARKET_DATA_URL}/quote/${basket.toLowerCase()}${q({ side, amount: amount.toString(), slippageBps })}`,
  );

// display formatters (numbers already in display units)
export const fmtUsdCompact = (n: number) =>
  n.toLocaleString("en-US", { style: "currency", currency: "USD", notation: "compact", maximumFractionDigits: 1 });

export const fmtUsd = (n: number) =>
  n.toLocaleString("en-US", {
    style: "currency",
    currency: "USD",
    maximumFractionDigits: n !== 0 && Math.abs(n) < 1 ? 4 : 2,
  });

export const fmtPct = (n: number, digits = 2) => `${n.toFixed(digits)}%`;

// -0.001 must not print as "-0.00%": round before deciding the sign.
export const fmtPctSigned = (n: number, digits = 2) => {
  const r = Number(n.toFixed(digits));
  return `${r > 0 ? "+" : ""}${r.toFixed(digits)}%`;
};

export const fmtBps = (bps: number) => `${(bps / 100).toFixed(2)}%`;

export const shortHash = (h: string) => (h.length > 14 ? `${h.slice(0, 8)}…${h.slice(-6)}` : h);

export const fmtTime = (iso: string | number) => {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? String(iso) : d.toLocaleString();
};

export const errMsg = (e: unknown): string =>
  (e as { shortMessage?: string })?.shortMessage ?? (e instanceof Error ? e.message : String(e));
