# indexer setup

Envio HyperIndex over HyperSync. Finalized history only: the LP PnL
decomposition, position/liquidation history, funding and borrow curves, oracle
publications, and Kuru spot fills. **Not** Perpl — Perpl serves
`/trading/fills`, `/order-history` and `/position-history` itself and is
authoritative for its own matching.

GraphQL is on **8090** (`HASURA_EXTERNAL_PORT`), not 8080: `wallet` owns 8080.
`INDEXER_URL=http://localhost:8090/v1/graphql`.

## 0. Prerequisites

| Need | Why |
|---|---|
| bun | `bun install`, `bunx envio`. No pnpm, no workspace: this package installs standalone. |
| Node 22+ on PATH | envio's CLI is a node binary with native addons and globs `src/handlers/**` using Node >=22 APIs. bun installs it fine; it runs under node. |
| Docker running | Postgres. `bunx envio local docker up` starts its own Postgres + Hasura. |
| A **HyperSync** API token | Hard requirement, see §1. |

## 1. `ENVIO_API_TOKEN` is required

Verified live (2026-09-21):

```
GET  https://monad-testnet.hypersync.xyz/height            -> 200 {"height":64317408}
GET  https://monad.hypersync.xyz/height                    -> 200 {"height":106616322}
POST https://monad-testnet.hypersync.xyz/query  (no auth)  -> 401
```

`/height` being open is misleading; `/query` is where the data is. There is no
degraded unauthenticated mode, so `src/handlers/preflight.ts` throws at startup
when the var is missing rather than letting the indexer sit at 0 blocks.

A HyperRPC token is a different product and gets `403` here. A `403` instead of
a `401` means the token is real but scoped wrong.

### If HyperSync is down or the token is revoked

envio can sync over plain JSON-RPC. This is deliberately **not** wired up — one
working path beats two half-wired ones — but it is tested. Paste into the
`chains[0]` block of `config.yaml`, next to `block_lag`:

```yaml
    rpc:
      url: ${ENVIO_RPC_URL:-https://testnet-rpc.monad.xyz}
      for: sync                 # omit, or use `fallback`, to keep HyperSync primary
      initial_block_interval: ${ENVIO_RPC_LOG_RANGE:-100}
      interval_ceiling: ${ENVIO_RPC_LOG_RANGE:-100}
```

`for: sync` makes RPC primary and no `ENVIO_API_TOKEN` is needed (drop the
preflight guard too). Both interval fields are load-bearing per provider:

| Provider | `ENVIO_RPC_LOG_RANGE` | Notes |
|---|---|---|
| `https://testnet-rpc.monad.xyz` | `100` | Hard `eth_getLogs is limited to a 100 range`, returned as an **error**, so envio's 2000-block default fails and it spends minutes halving down. Also rate-limits at 50 req/s. |
| `https://monad-testnet.rpc.hypersync.xyz/<token>` | `1000` | HyperRPC. No range cap observed, no 50/s ceiling. |

The HyperRPC token sits in the URL **path**, not a header, so the resulting
`ENVIO_RPC_URL` is itself a secret.

## 2. Environment

Project vars live in `main/.env`; symlink it, because envio loads `.env` from
its own cwd:

```sh
cd main/indexer && ln -s ../.env .env
```

| Var | Absent → |
|---|---|
| `ENVIO_API_TOKEN` | preflight throws. Nothing indexes. |
| `ENVIO_START_BLOCK` | defaults to `0`; correct but rescans the chain. |
| `KURU_MARKETS` | falls back to the four live testnet markets. Format is a YAML flow list: `["0xaa..","0xbb.."]`. A bare comma list fails codegen with `invalid address`. |
| `KURU_ACCOUNT_CORE` | falls back to `0x6384e9b2Bf3b65e1535403a0A543b5FDA905eE22`. Absent from `.env` is the normal case. |
| `HASURA_EXTERNAL_PORT` | GraphQL lands on 8080 and collides with `wallet`. Set `8090`. |
| `ENVIO_BLOCK_LAG` | **do not set.** Config default `5` = FINALITY_DEPTH. Only blocks ≥5 behind head are indexed, which is what makes `rollback_on_reorg: false` correct rather than reckless. `bun run test` sets `0` because the test framework's synthetic block source never advances past the last simulated block and a non-zero lag hangs forever. Nothing else may lower it. |

Postgres and runtime vars are envio's own defaults (`ENVIO_PG_*`, `ENVIO_TUI`,
`ENVIO_HASURA`, `ENVIO_INDEXER_PORT` — `/healthz` and `/metrics` on 9898).

## 3. Run it

```sh
cd main/indexer
bun install
bunx envio codegen     # regenerates .envio/types.d.ts
bun run test           # abi drift + scale + kuru decoder, no network
bunx envio local docker up   # envio-postgres (ENVIO_PG_PORT) + envio-hasura (8090)
bunx envio start             # codegen + migrate + track Hasura + index
```

Env must be loaded (`set -a; . ../.env; set +a`) — `.env` needs
`ENVIO_PG_PORT=5434` (ours owns 5433) and
`HASURA_GRAPHQL_ENDPOINT=http://localhost:8090/v1/metadata`.

**Not `envio dev`.** envio 3.9.0's `dev` waits on Hasura health at a
hardcoded `http://localhost:8080//hasura/healthz`
(`packages/cli/src/service_health.rs`, `HASURA_ENDPOINT` const), ignoring
`HASURA_EXTERNAL_PORT`. `wallet` owns 8080 and 404s it, so `dev` hangs silently
for ~2h (1s..120s linear backoff) before failing. `start` is `dev` minus Docker
and that check.

`bun` is the only supported package manager here. envio's docs recommend pnpm;
bun installs the `envio-darwin-arm64` native addon and runs `codegen` fine.

## 4. Verify it indexed

```sh
docker exec envio-postgres psql -U postgres -d envio-dev \
  -c 'select count(*) from public."KuruFill";' \
  -c 'select count(*) from public."KuruSlot";' \
  -c 'select id,free,reserved from public."KuruBalance" limit 5;'
```

Kuru rows stay in raw venue units (see schema.graphql). Zero rows means the
bindings are wrong — run the drift test.

Sync health:

```sh
curl -s localhost:9898/healthz
```

`source_block - progress_block` in `envio_chains` should sit at ~5
(`block_lag`) and `_is_hyper_sync` should be `t`.

## 5. When our contracts change

`envio codegen` validates that the signatures in `config.yaml` *parse*, never
that they still describe the deployed contract. A wrong signature changes
`topic0`, so the indexer matches nothing and reports no error. That failure has
shipped before (`VaultDeposit`/`VaultWithdraw`, which never existed — the vault
is an ERC-4626 and emits OpenZeppelin's `Deposit`/`Withdraw`; and `uint80
roundId`, which is `uint64` on chain).

`src/abi-drift.test.ts` closes the gap: it parses the `event` declarations out
of `../contracts/src/*.sol`, expands structs, resolves contract-typed params to
`address`, and requires every `config.yaml` signature to match one.

```sh
bunx vitest run src/abi-drift.test.ts
```

If a contract starts emitting an event declared somewhere new, add that file to
the `SOURCES` map at the top of the test.

## 6. Kuru Spot V2

There is no Solidity for Kuru in this repo, so `abi-drift.test.ts` pins the
**topic0 the deployed contracts actually emit** instead. Re-read them from the
chain with:

```sh
curl -s -X POST https://monad-testnet.hypersync.xyz/query \
  -H "authorization: Bearer $ENVIO_API_TOKEN" -H 'content-type: application/json' \
  -d '{"from_block":64699420,"to_block":64699422,
       "logs":[{"address":["0xa9C2936656a7D2143720BcD91Ba8506200B7CbE7"]}],
       "field_selection":{"log":["block_number","log_index","topic0","topic1","topic2","data"]}}'
```

Only two topic0s come back, and that is the whole point: **Spot V2 emits no
`OrderCreated`, `Trade` or `OrdersCanceled`.** Those are V1. 100 000 blocks of
WETH/USDC contain exactly `BookUpdatesPacked` (943) and `TradesPacked` (90).
`accountId` and `executor` are indexed; the `bytes` payload carries everything
else, 39 bytes per book record and 64 per trade record. `src/kuru-packed.ts`
decodes them and fails closed on a bad length, a reserved bit or an unknown
flag — one bad record makes every later byte offset a guess.
`src/kuru-packed.test.ts` decodes real logs and asserts the Data Source API's
own numbers for the same trade.

Ordering is `(blockNumber, txIndex, logIndex, byteOffset)`. `logIndex` is
block-scoped so it subsumes `txIndex`, and `recordIdx` is the byte offset
ordinal, so `(blockNumber, logIndex, recordIdx)` is that same total order and is
what the rows carry.

Balances come from **AccountCore**, not the orderbooks — orderbooks never hold
tokens. `SpotReserveUpdated` is a post-state snapshot of `(free, reserved)` and
is the only writer. `Deposit` / `Withdrawal` / `InternalAccountTransfer` are
history for the same movements and are deliberately not bound: applying both
double-counts.

Not indexed, on purpose:

| | Why |
|---|---|
| SpotRouter `0xba24a1042701f06e8F7edCF04389260D1Fa4c697` | registry only. Market discovery duplicates `KURU_MARKETS`, and §10.3 of the spec says not to trust router registration without AccountCore confirming it anyway. |
| KuruTradingWallet `0xc7f2a9761276F7050D6561d2FDC51abC993F45E9` | the log address is the **delegated EOA**, never this implementation, so an address filter matches nothing; catching `IntentExecuted` needs a wildcard scan and nothing here reads it. |
| Passive bands, hooks, triggers, L2 levels | no consumer. The gateway serves L2; `getL2Book` serves depth. |
| Executable-depth (RAB) model | `minSizeAfterBlock` is stored per slot, but no event fires at threshold+1, so a depth model would have to schedule its own derived transition per order and unschedule it on reorg. `KuruSlot` is the **stored** book; derive executable depth at read time. |

Price and size stay in the market's own `uint32`/`uint96` tick units.
`pricePrecision`, `sizePrecision` and `baseSizeMultiplier` appear in no event —
`rawBaseAtoms = size * baseSizeMultiplier` needs a contract read, so the
consumer scales. Storing a value we cannot verify as if it were scaled would be
worse than storing it raw.
