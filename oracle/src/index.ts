import { connect as natsConnect, type NatsConnection } from "nats";
import { type Address, createPublicClient, createWalletClient, defineChain, http } from "viem";
import { privateKeyToAccount } from "viem/accounts";
import { monadTestnet } from "viem/chains";
import { loadEnv, powerMarketId, pythFeedId, rpcUrl, volMarketId } from "./config.ts";
import { connect as dbConnect, insertSample, migrate, recentPrices } from "./db.ts";
import { Publisher } from "./publisher.ts";
import { chainlinkSource } from "./sources/chainlink.ts";
import { pythSource } from "./sources/pyth.ts";
import { IndexState } from "./state.ts";

const env = loadEnv();
const log = (o: Record<string, unknown>) => console.log(JSON.stringify({ t: new Date().toISOString(), ...o }));

const monad = defineChain({
  id: 143,
  name: "monad",
  nativeCurrency: { name: "MON", symbol: "MON", decimals: 18 },
  rpcUrls: { default: { http: [env.MONAD_RPC_URL_143] } },
});
const chain = (id: number) => (id === 10143 ? monadTestnet : monad);
const publicClientFor = (id: number) =>
  createPublicClient({ chain: chain(id), transport: http(rpcUrl(env, id)) });

// reads and writes are on different chains on purpose: pyth is only fresh on 143.
const readChain = env.ORACLE_PRICE_SOURCE === "pyth" ? env.PYTH_READ_CHAIN_ID : env.CHAIN_ID;
const reader = publicClientFor(readChain);
const writerClient = publicClientFor(env.CHAIN_ID);

const source =
  env.ORACLE_PRICE_SOURCE === "pyth"
    ? pythSource(reader, env.PYTH_CONTRACT as Address, pythFeedId(env), env.ORACLE_STALE_MS)
    : chainlinkSource(reader, env.CHAINLINK_ETH_USD_10143 as Address, env.ORACLE_STALE_MS);

const wallet = env.PROTOCOL_SIGNER_KEY
  ? createWalletClient({
      account: privateKeyToAccount(env.PROTOCOL_SIGNER_KEY as `0x${string}`),
      chain: chain(env.CHAIN_ID),
      transport: http(rpcUrl(env, env.CHAIN_ID)),
    })
  : null;

const oracles: Record<string, string | undefined> = {
  [volMarketId(env.ORACLE_UNDERLYING)]: env.VOL_INDEX_ORACLE,
  [powerMarketId(env.ORACLE_UNDERLYING, 2n * 10n ** 18n)]: env.POWER2_INDEX_ORACLE,
  [powerMarketId(env.ORACLE_UNDERLYING, 5n * 10n ** 17n)]: env.POWER_HALF_INDEX_ORACLE,
};

const sql = dbConnect(env.DATABASE_URL);
await migrate(sql);

// the bus is not on the critical path: a nats outage must not stop us pushing the
// index positions settle against.
const nc: NatsConnection | null = env.NATS_URL
  ? await natsConnect({ servers: env.NATS_URL }).catch((err: unknown) => {
      log({ level: "error", msg: "nats unavailable, continuing without events", err: String(err) });
      return null;
    })
  : null;

const state = new IndexState(env);
state.seed(await recentPrices(sql, env.ORACLE_UNDERLYING, env.ORACLE_WARMUP_SAMPLES * 4));
log({ msg: "ewma seeded from history", samples: state.ewma.samples });

const publisher = new Publisher(env, writerClient, wallet, sql, nc, (m) => oracles[m] as Address | undefined);

let lastTickOk: number | null = null;
let lastError: string | null = null;
let ticking = false;

async function tick(): Promise<void> {
  const sample = await source.read();
  await insertSample(sql, source.name, env.ORACLE_UNDERLYING, sample);
  state.ingest(sample);
  if (!state.warm) {
    log({ msg: "warming up", samples: state.ewma.samples, of: env.ORACLE_WARMUP_SAMPLES });
    return;
  }
  for (const feed of state.feeds()) {
    const outcome = await publisher.publish(feed);
    log({
      msg: "index",
      marketId: feed.marketId,
      value: feed.value.toString(),
      fundingTerm: feed.fundingTerm?.toString(),
      outcome,
    });
  }
}

// a fixed interval is load-bearing: lambda=0.94 is calibrated to evenly spaced
// samples, so a skipped tick is preferable to a bunched one.
const timer = setInterval(() => {
  if (ticking) return;
  ticking = true;
  void tick()
    .then(
      () => {
        lastTickOk = Date.now();
        lastError = null;
      },
      (err: unknown) => {
        lastError = err instanceof Error ? err.message : String(err);
        log({ level: "error", msg: "tick failed", err: lastError });
      },
    )
    .finally(() => {
      ticking = false;
    });
}, env.ORACLE_SAMPLE_INTERVAL_MS);

const server = Bun.serve({
  port: env.ORACLE_PORT,
  fetch: (req) =>
    new URL(req.url).pathname === "/health"
      ? Response.json({
          ok: lastError === null && state.warm,
          source: source.name,
          readChain,
          warm: state.warm,
          samples: state.ewma.samples,
          sigma: state.sigma.toString(),
          lastTickOk,
          lastError,
        })
      : new Response("not found", { status: 404 }),
});

log({ msg: "oracle up", port: env.ORACLE_PORT, source: source.name, readChain, writeChain: env.CHAIN_ID });

for (const sig of ["SIGINT", "SIGTERM"] as const) {
  process.once(sig, () => {
    void (async () => {
      clearInterval(timer);
      await server.stop();
      await nc?.drain().catch(() => {});
      await sql.end();
      process.exit(0);
    })();
  });
}
