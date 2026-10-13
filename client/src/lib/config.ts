import { createPublicClient, http } from "viem";
import { monad } from "viem/chains";
import deployments from "@/abi/deployments.json";

// NEXT_PUBLIC_* is baked in at build time — a dev-server restart is not enough.
const RPC_URL = process.env.NEXT_PUBLIC_RPC_URL ?? "https://rpc.monad.xyz";
export const API_URL = `${process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080"}/api/v1`;
export const MARKET_DATA_URL = `${process.env.NEXT_PUBLIC_MARKET_DATA_URL ?? "http://localhost:8082"}/api/v1`;
export const WS_URL = process.env.NEXT_PUBLIC_MARKET_DATA_WS_URL ?? "ws://localhost:8082/api/v1/ws";
export const HISTORY_URL = `${process.env.NEXT_PUBLIC_HISTORY_URL ?? "http://localhost:8087"}/api/v1`;
export const PRIVY_APP_ID = process.env.NEXT_PUBLIC_PRIVY_APP_ID ?? "";
export const SIGNER_ID = process.env.NEXT_PUBLIC_PRIVY_SIGNER_ID ?? "";
export const POLICY_ID = process.env.NEXT_PUBLIC_PRIVY_POLICY_ID ?? "";

export const chain = { ...monad, rpcUrls: { default: { http: [RPC_URL] } } } as const;

const EXPLORER = monad.blockExplorers.default.url;
export const txUrl = (hash: string) => `${EXPLORER}/tx/${hash}`;
export const addressUrl = (addr: string) => `${EXPLORER}/address/${addr}`;

export const DEPLOYMENTS = deployments;
export const TOTAL_BPS = 10000;
export const FEE_BPS = 40;

export const publicClient = createPublicClient({ chain, transport: http(RPC_URL) });
