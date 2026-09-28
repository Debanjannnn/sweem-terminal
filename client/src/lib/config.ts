import { parseAbi, type Abi, type Address } from "viem";
import { monadTestnet } from "viem/chains";
import deployments from "@/abi/deployments.json";
import basketJson from "@/abi/SweemBasket.json";
import factoryJson from "@/abi/SweemBasketFactory.json";

// NEXT_PUBLIC_* is baked in at build time — a dev-server restart is not enough.
export const RPC_URL = process.env.NEXT_PUBLIC_RPC_URL ?? "https://testnet-rpc.monad.xyz";
export const API_URL = `${process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080"}/api/v1`;
export const MARKET_DATA_URL = `${process.env.NEXT_PUBLIC_MARKET_DATA_URL ?? "http://localhost:8082"}/api/v1`;
export const WS_URL = process.env.NEXT_PUBLIC_MARKET_DATA_WS_URL ?? "ws://localhost:8082/api/v1/ws";
export const PRIVY_APP_ID = process.env.NEXT_PUBLIC_PRIVY_APP_ID ?? "";
export const SIGNER_ID = process.env.NEXT_PUBLIC_PRIVY_SIGNER_ID ?? "";
export const POLICY_ID = process.env.NEXT_PUBLIC_PRIVY_POLICY_ID ?? "";

export const chain = {
  ...monadTestnet,
  rpcUrls: { default: { http: [RPC_URL] } },
} as const;

export const EXPLORER = "https://testnet.monadexplorer.com";
export const txUrl = (hash: string) => `${EXPLORER}/tx/${hash}`;
export const addressUrl = (addr: string) => `${EXPLORER}/address/${addr}`;

export const FACTORY = deployments.factory as Address;
export const USDC = deployments.kuru.usdc as Address;
export const FAUCET = deployments.kuru.faucet as Address;
export const USDC_DECIMALS = 6;
export const SHARE_DECIMALS = 18;
export const TOTAL_BPS = 10000;

export const basketAbi = basketJson as Abi;
export const factoryAbi = factoryJson as Abi;
export const faucetAbi = parseAbi([
  "function claim()",
  "function nextClaimAt(address) view returns (uint256)",
]);
