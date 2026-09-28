import { parseUnits } from "viem";
import { z } from "zod";

const address = z.string().regex(/^0x[0-9a-fA-F]{40}$/);
const bytes32 = z.string().regex(/^0x[0-9a-fA-F]{64}$/);

// "0.94" -> 94e16
const toWad = (v: string, ctx: z.RefinementCtx): bigint => {
  try {
    return parseUnits(v.trim(), 18);
  } catch {
    ctx.addIssue({ code: "custom", message: `not a decimal: ${v}` });
    return z.NEVER;
  }
};

const wad = (fallback: string) => z.string().default(fallback).transform(toWad);

const schema = z.object({
  CHAIN_ID: z.coerce.number().int().default(10143),
  // pyth is a pull oracle: freshness is a property of the chain, not the feed.
  // monad mainnet updates every 5-7s, testnet is ~68h stale. so we read the price
  // from mainnet whatever chain we trade on — ETH does not cost less on a testnet.
  PYTH_READ_CHAIN_ID: z.coerce.number().int().default(143),
  MONAD_RPC_URL_143: z.string().default("https://rpc.monad.xyz"),
  MONAD_RPC_URL_10143: z.string().default("https://testnet-rpc.monad.xyz"),

  ORACLE_PRICE_SOURCE: z.enum(["pyth", "chainlink"]).default("pyth"),
  ORACLE_UNDERLYING: z.enum(["ETH/USD", "MON/USD", "BTC/USD"]).default("ETH/USD"),
  PYTH_CONTRACT: address.default("0x2880aB155794e7179c9eE2e38200202908C17B43"),
  PYTH_FEED_ETH_USD: bytes32.default("0xff61491a931112ddf1bd8147cd1b641375f79f5825126d665480874634fd0ace"),
  PYTH_FEED_MON_USD: bytes32.default("0x31491744e2dbf6df7fcf4ac0820d18a609b49076d45066d3568424e62f686cd1"),
  PYTH_FEED_BTC_USD: bytes32.default("0xe62df6c8b4a85fe1a67db44dc12de5db330f7ac66b72dc658afedf0f4a415b43"),
  // chainlink ETH/USD on 10143. ~4min freshness.
  CHAINLINK_ETH_USD_10143: address.default("0x5c8c8482f064049248F86D9F4aFa4B1f2F5b6d31"),

  ORACLE_SAMPLE_INTERVAL_MS: z.coerce.number().int().positive().default(15_000),
  // getPriceUnsafe does no staleness check; this is the only thing between us and a
  // 68-hour-old price becoming sigma.
  ORACLE_STALE_MS: z.coerce.number().int().positive().default(60_000),
  // riskmetrics daily default. half-life ~11 samples, calibrated to a fixed interval.
  ORACLE_LAMBDA: wad("0.94"),
  ORACLE_WARMUP_SAMPLES: z.coerce.number().int().positive().default(30),
});

export type OracleEnv = z.infer<typeof schema>;

export function loadEnv(source: Record<string, string | undefined> = process.env): OracleEnv {
  const parsed = schema.safeParse(source);
  if (!parsed.success) {
    const issues = parsed.error.issues.map((i) => `${i.path.join(".")}: ${i.message}`).join("\n  ");
    throw new Error(`invalid oracle env:\n  ${issues}`);
  }
  return parsed.data;
}

export const rpcUrl = (env: OracleEnv, chainId: number): string => {
  const url = chainId === 143 ? env.MONAD_RPC_URL_143 : chainId === 10143 ? env.MONAD_RPC_URL_10143 : undefined;
  if (!url) throw new Error(`no rpc url for chain ${chainId}`);
  return url;
};

export const pythFeedId = (env: OracleEnv): `0x${string}` =>
  ({
    "ETH/USD": env.PYTH_FEED_ETH_USD,
    "MON/USD": env.PYTH_FEED_MON_USD,
    "BTC/USD": env.PYTH_FEED_BTC_USD,
  })[env.ORACLE_UNDERLYING] as `0x${string}`;
