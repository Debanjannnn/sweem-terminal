import { getAddress } from "viem";

// Aurora Intents (NEAR Intents 1Click): swap any asset on 30+ chains into USDC on Monad. The app key is public by design
// (Aurora docs) and the API allows browser origins, so the client calls it directly.
const BASE = "https://intents-api.aurora.dev/api";
export const AURORA_KEY = process.env.NEXT_PUBLIC_AURORA_APP_KEY ?? "";

export type AuroraToken = { assetId: string; decimals: number; blockchain: string; symbol: string; price: number; contractAddress?: string };

export type AuroraQuote = {
  quote: {
    timeEstimate: number;
    amountIn: string;
    amountInFormatted: string;
    amountInUsd: string;
    minAmountIn: string;
    amountOut: string;
    amountOutFormatted: string;
    amountOutUsd: string;
    minAmountOut: string;
    depositAddress?: string;
    depositMemo?: string;
    deadline?: string;
  };
};

type AuroraStatus = "KNOWN_DEPOSIT_TX" | "PENDING_DEPOSIT" | "INCOMPLETE_DEPOSIT" | "PROCESSING" | "SUCCESS" | "REFUNDED" | "FAILED";
type TxRef = string | { hash: string; explorerUrl?: string };
export type AuroraSwap = {
  status: AuroraStatus;
  updatedAt: string;
  swapDetails?: { amountOutFormatted?: string; refundReason?: string; originChainTxHashes: TxRef[]; destinationChainTxHashes: TxRef[] };
};

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}/${path}`, init);
  const body = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(body?.message ?? `Aurora ${res.status}`);
  return body as T;
}

export const getAuroraTokens = () => call<{ tokens: AuroraToken[] }>(`tokens/${AURORA_KEY}`).then((r) => r.tokens);

// recipient gets USDC on Monad; refunds (expired / short deposits) go back to refundTo on the origin chain
export const auroraQuote = (b: { dry: boolean; exactOut: boolean; amount: string; origin: string; destination: string; recipient: string; refundTo: string }) =>
  call<AuroraQuote>(`quote/${AURORA_KEY}`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      dry: b.dry,
      swapType: b.exactOut ? "EXACT_OUTPUT" : "EXACT_INPUT",
      depositType: "ORIGIN_CHAIN",
      amount: b.amount,
      originAsset: b.origin,
      destinationAsset: b.destination,
      slippageTolerance: 100,
      refundTo: b.refundTo,
      refundType: "ORIGIN_CHAIN",
      recipient: b.recipient,
      recipientType: "DESTINATION_CHAIN",
    }),
  });

export const auroraStatus = (depositAddress: string, memo?: string) =>
  call<AuroraSwap>(`status/${AURORA_KEY}?depositAddress=${encodeURIComponent(depositAddress)}${memo ? `&depositMemo=${encodeURIComponent(memo)}` : ""}`);

export const txHash = (t: TxRef) => (typeof t === "string" ? t : t.hash);

// chains where the user's EVM wallet address is valid (refund target without asking)
export const EVM = new Set(["eth", "base", "arb", "op", "pol", "bsc", "avax", "gnosis", "bera", "scroll", "xlayer", "plasma", "abs", "hood", "adi"]);

const CHAIN: Record<string, string> = {
  eth: "Ethereum",
  base: "Base",
  arb: "Arbitrum",
  op: "Optimism",
  pol: "Polygon",
  bsc: "BNB Chain",
  avax: "Avalanche",
  sol: "Solana",
  btc: "Bitcoin",
  tron: "Tron",
  near: "NEAR",
  sui: "Sui",
  ton: "TON",
  aptos: "Aptos",
  gnosis: "Gnosis",
  bera: "Berachain",
  scroll: "Scroll",
  xlayer: "X Layer",
  plasma: "Plasma",
  abs: "Abstract",
  hood: "Robinhood",
  stellar: "Stellar",
  xrp: "XRP Ledger",
  doge: "Dogecoin",
  ltc: "Litecoin",
  bch: "Bitcoin Cash",
  zec: "Zcash",
  cardano: "Cardano",
  starknet: "Starknet",
  hypercore: "Hyperliquid",
};
export const chainName = (c: string) => CHAIN[c] ?? c.toUpperCase();
// popular first, the rest alphabetical
const ORDER = ["eth", "base", "arb", "sol", "btc", "op", "pol", "bsc", "avax", "tron", "near"];
export const chainRank = (c: string) => (ORDER.includes(c) ? ORDER.indexOf(c) : ORDER.length);

// Aurora tokens carry no logo: Trust Wallet's per-chain logo (native coin or contract), then coincap by symbol.
// Callers try them in order and fall back to a letter. ponytail: public CDNs, no hosting; ~60% of the list has a logo.
const TW: Record<string, string> = {
  eth: "ethereum", base: "base", arb: "arbitrum", op: "optimism", pol: "polygon", bsc: "smartchain", avax: "avalanchec", gnosis: "xdai",
  sol: "solana", tron: "tron", btc: "bitcoin", doge: "doge", xrp: "xrp", sui: "sui", cardano: "cardano", ltc: "litecoin",
  bch: "bitcoincash", zec: "zcash", stellar: "stellar", aptos: "aptos", near: "near", ton: "ton", starknet: "starknet",
};
export function tokenLogos(t: AuroraToken): string[] {
  const out: string[] = [];
  const chain = TW[t.blockchain];
  if (chain) {
    const base = `https://raw.githubusercontent.com/trustwallet/assets/master/blockchains/${chain}`;
    const a = t.contractAddress;
    if (!a) out.push(`${base}/info/logo.png`);
    else if (a.startsWith("0x")) {
      try {
        out.push(`${base}/assets/${getAddress(a)}/logo.png`);
      } catch {}
    } else out.push(`${base}/assets/${a}/logo.png`);
  }
  // wrapped / staked tokens (wNEAR, stNEAR, wBTC) fall back to the underlying coin's logo
  for (const sym of new Set([t.symbol, t.symbol.replace(/^(w|st)(?=[A-Z])/, "")]))
    out.push(`https://assets.coincap.io/assets/icons/${sym.toLowerCase().replace(/[^a-z0-9]/g, "")}@2x.png`);
  return out;
}
