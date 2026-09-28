"use client";

import { useState } from "react";
import {
  QueryClient,
  QueryClientProvider,
  useMutation,
  useQuery,
  useQueryClient,
  type UseQueryOptions,
} from "@tanstack/react-query";
import { useSession } from "./session";
import {
  marketFetch,
  walletFetch,
  type Account,
  type AuthorizeResult,
  type Book,
  type Candle,
  type Delegation,
  type DepositResult,
  type Execution,
  type Market,
  type Mark,
  type Order,
  type OrderResponse,
  type PerplAccount,
  type Portfolio,
  type Quote,
  type Schedule,
} from "./api";

// the ws pushes books, ticks and candles, so nothing here polls them
const MINUTE = 60_000;

export function useMarkets() {
  return useQuery({
    queryKey: ["markets"],
    queryFn: () => marketFetch<Market[]>("/v1/markets"),
    staleTime: 5 * MINUTE,
  });
}

export function useMarks(ids: string[]) {
  const key = [...ids].sort().join(",");
  return useQuery({
    queryKey: ["marks", key],
    queryFn: () => marketFetch<{ marks: Mark[] }>(`/v1/marks?markets=${encodeURIComponent(key)}`),
    enabled: key !== "",
    staleTime: 15_000,
  });
}

export function useBook(marketId: string | null) {
  return useQuery({
    queryKey: ["book", marketId],
    queryFn: () => marketFetch<Book>(`/v1/book/${encodeURIComponent(marketId!)}`),
    enabled: !!marketId,
    staleTime: Infinity,
    retry: false,
  });
}

export function useCandles(marketId: string | null, resolution: number, from: number, to: number) {
  return useQuery({
    queryKey: ["candles", marketId, resolution, from, to],
    queryFn: () =>
      marketFetch<Candle[]>(
        `/v1/candles/${encodeURIComponent(marketId!)}?resolution=${resolution}&from=${from}&to=${to}`,
      ),
    enabled: !!marketId,
    staleTime: MINUTE,
  });
}

export function useQuotePreview(marketId: string | null, side: string, sizeWad: string) {
  return useQuery({
    queryKey: ["quote", marketId, side, sizeWad],
    queryFn: () =>
      marketFetch<Quote>("/v1/quote", {
        method: "POST",
        body: JSON.stringify({ marketId, side, size: sizeWad }),
      }),
    enabled: !!marketId && sizeWad !== "0" && sizeWad !== "",
    staleTime: 5_000,
    retry: false,
  });
}

function useAuthedQuery<T>(
  key: unknown[],
  path: string,
  options?: Partial<UseQueryOptions<T, Error, T, readonly unknown[]>>,
) {
  const { authenticated, ready, token, me, error } = useSession();
  // wait for the POST /v1/me sync, else every read answers 412 user_not_registered
  const registered = me !== null || error !== null;
  return useQuery<T, Error, T, readonly unknown[]>({
    queryKey: key,
    queryFn: async () => walletFetch<T>(path, await token()),
    staleTime: 10_000,
    ...options,
    enabled: ready && authenticated && registered && (options?.enabled ?? true) !== false,
  });
}

export const useOrders = () =>
  useAuthedQuery<{ orders: Order[] }>(["orders"], "/v1/orders");

export const useExecutions = () =>
  useAuthedQuery<{ executions: Execution[] }>(["executions"], "/v1/executions");

export const usePortfolio = () => useAuthedQuery<Portfolio>(["portfolio"], "/v1/portfolio");

export const useSchedules = () =>
  useAuthedQuery<{ schedules: Schedule[] }>(["schedules"], "/v1/schedules");

// the list omits slices; only the single-schedule read carries them
export const useSchedule = (id: string | null) =>
  useAuthedQuery<Schedule>(["schedule", id], `/v1/schedules/${id}`, { enabled: !!id });

// 404 here means "not delegated", which is a state, not a failure to retry
export const useDelegation = () =>
  useAuthedQuery<Delegation>(["delegation"], "/v1/delegation", { retry: false });

// 412 here is an unmet precondition, not a failure to retry: its message names
// what is missing and is rendered verbatim
export const useAccount = () =>
  useAuthedQuery<Account>(["account"], "/v1/account", { retry: false });

export function useDeposit() {
  const { token } = useSession();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (input: { market_id: string; token: "base" | "quote"; amount: string }) =>
      walletFetch<DepositResult>("/v1/account/deposit", await token(), {
        method: "POST",
        body: JSON.stringify(input),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["account"] }),
  });
}

export function useAuthorize() {
  const { token } = useSession();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (expiry: number) =>
      walletFetch<AuthorizeResult>("/v1/account/authorize", await token(), {
        method: "POST",
        body: JSON.stringify({ expiry }),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["account"] }),
  });
}

// revoking advances the authorization epoch, which invalidates every pending
// automated order, so the open-order list is stale too
export function useRevokeAuthorization() {
  const { token } = useSession();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () =>
      walletFetch<AuthorizeResult>("/v1/account/authorize", await token(), { method: "DELETE" }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["account"] });
      qc.invalidateQueries({ queryKey: ["orders"] });
    },
  });
}

export type OrderInput = {
  market_id: string;
  venue: string;
  side: "buy" | "sell";
  chain_id: number;
  idempotency_key: string;
} & (
  | { order_type: "market"; size: string; max_slippage_bps: number }
  // perpl: quantity in lots, price in PNS, leverage in hundredths
  | { order_type: "perp_market"; quantity: string; leverage: number; max_slippage_bps: number }
  | { order_type: "perp_limit"; price: string; quantity: string; leverage: number; post_only: boolean }
  | { order_type: "close"; quantity?: string; max_slippage_bps: number }
  | {
      order_type: "limit";
      price: string;
      quantity: string;
      tif: "gtc" | "ioc" | "fok";
      post_only: boolean;
    }
);

export function useCreateOrder() {
  const { token } = useSession();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (input: OrderInput) =>
      walletFetch<OrderResponse>("/v1/orders", await token(), {
        method: "POST",
        // the wallet names perp orders market/limit too; the venue tells them apart
        body: JSON.stringify({
          ...input,
          order_type: input.order_type.replace(/^perp_/, ""),
        }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["orders"] });
      qc.invalidateQueries({ queryKey: ["executions"] });
      qc.invalidateQueries({ queryKey: ["portfolio"] });
      qc.invalidateQueries({ queryKey: ["perpl-account"] });
    },
  });
}

// a slot is reused, so a cancel is bound to the order the caller believes is in it
export function useCancelOrder() {
  const { token } = useSession();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (o: { market_id: string; slot_idx: number; expected_order_id: string }) =>
      walletFetch<OrderResponse>(
        `/v1/orders/${encodeURIComponent(o.market_id)}/${o.slot_idx}?expected_order_id=${encodeURIComponent(o.expected_order_id)}`,
        await token(),
        { method: "DELETE" },
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["orders"] });
      qc.invalidateQueries({ queryKey: ["executions"] });
    },
  });
}

export const usePerplAccount = () =>
  useAuthedQuery<PerplAccount>(["perpl-account"], "/v1/perpl/account", { retry: false });

function usePerplFunds(path: "/v1/perpl/deposit" | "/v1/perpl/withdraw") {
  const { token } = useSession();
  const qc = useQueryClient();
  return useMutation({
    // amount in collateral atoms
    mutationFn: async (amount: string) =>
      walletFetch<OrderResponse>(path, await token(), {
        method: "POST",
        body: JSON.stringify({ amount, idempotency_key: crypto.randomUUID() }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["perpl-account"] });
      qc.invalidateQueries({ queryKey: ["portfolio"] });
      qc.invalidateQueries({ queryKey: ["orders"] });
    },
  });
}

export const usePerplDeposit = () => usePerplFunds("/v1/perpl/deposit");
export const usePerplWithdraw = () => usePerplFunds("/v1/perpl/withdraw");

// perpl order ids are recycled; the executor refuses unless the order is still ours
export function useCancelPerplOrder() {
  const { token } = useSession();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (o: { market_id: string; order_id: string }) =>
      walletFetch<OrderResponse>(
        `/v1/perpl/orders/${encodeURIComponent(o.market_id)}/${encodeURIComponent(o.order_id)}`,
        await token(),
        { method: "DELETE" },
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["orders"] });
      qc.invalidateQueries({ queryKey: ["executions"] });
      qc.invalidateQueries({ queryKey: ["perpl-account"] });
    },
  });
}

export type ScheduleInput = {
  kind: "twap" | "dca" | "grid";
  market_id: string;
  venue: string;
  side: "buy" | "sell";
  total: string;
  slice_count?: number;
  window_seconds?: number;
  cadence_seconds?: number;
  levels?: unknown;
  idempotency_key: string;
};

export function useCreateSchedule() {
  const { token } = useSession();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (input: ScheduleInput) =>
      walletFetch<Schedule>("/v1/schedules", await token(), {
        method: "POST",
        body: JSON.stringify(input),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["schedules"] }),
  });
}

export function useCancelSchedule() {
  const { token } = useSession();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) =>
      walletFetch<{ cancelled: boolean }>(`/v1/schedules/${id}`, await token(), {
        method: "DELETE",
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["schedules"] }),
  });
}

export type DelegationInput = {
  signer_id: string;
  policy_id: string;
  venues: string[];
  markets: string[];
  max_notional_per_slice: string;
  max_notional_total: string;
  expires_at: string;
};

export function usePutDelegation() {
  const { token } = useSession();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (input: DelegationInput) =>
      walletFetch<Delegation>("/v1/delegation", await token(), {
        method: "PUT",
        body: JSON.stringify(input),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["delegation"] }),
  });
}

export function useRevokeDelegation() {
  const { token } = useSession();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () =>
      walletFetch<{ revoked: boolean }>("/v1/delegation", await token(), { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["delegation"] }),
  });
}

export function QueryProvider({ children }: { children: React.ReactNode }) {
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: { refetchOnWindowFocus: false, retry: 1, staleTime: 10_000 },
        },
      }),
  );
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
