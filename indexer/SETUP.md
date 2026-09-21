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
| Docker running | Postgres. `bunx envio dev` starts its own Postgres + Hasura. |
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
| `SWEEM_VAULT` | zero address; no LP curve, no share price. |
| `MARKET_REGISTRY` | zero address; no markets — **and no oracles**, since feeds are discovered from `MarketAdded`. |
| `INDEX_PERP_ENGINE` | zero address; no positions, no funding/borrow. |
| `KURU_MARKETS` | no spot fills. Format is a YAML flow list: `["0xaa..","0xbb.."]`. A bare comma list fails codegen with `invalid address`. |
| `HASURA_EXTERNAL_PORT` | GraphQL lands on 8080 and collides with `wallet`. Set `8090`. |
| `ENVIO_BLOCK_LAG` | **do not set.** Config default `5` = FINALITY_DEPTH. Only blocks ≥5 behind head are indexed, which is what makes `rollback_on_reorg: false` correct rather than reckless. `bun run test` sets `0` because the test framework's synthetic block source never advances past the last simulated block and a non-zero lag hangs forever. Nothing else may lower it. |

There is no `INDEX_ORACLE` var. The deployment has three oracle contracts and
every one is registered dynamically by `contractRegister` on `MarketAdded`,
which carries both an `indexOracle` and a `volOracle` per market. Markets
sharing a vol oracle hand the same address to `IndexOracle.add` twice; envio's
address store dedupes it.

Postgres and runtime vars are envio's own defaults (`ENVIO_PG_*`, `ENVIO_TUI`,
`ENVIO_HASURA`, `ENVIO_INDEXER_PORT` — `/healthz` and `/metrics` on 9898).

## 3. Run it

```sh
cd main/indexer
bun install
bunx envio codegen     # regenerates .envio/types.d.ts
bun run test           # abi drift + scale + vault reducer, no network
bunx envio dev         # brings up its own Postgres + Hasura on 8090
```

`bun` is the only supported package manager here. envio's docs recommend pnpm;
bun installs the `envio-darwin-arm64` native addon and runs `codegen` fine.

## 4. Verify it indexed

```sh
docker exec envio-postgres psql -U postgres -d envio-dev \
  -c 'select "totalAssets","totalShares","sharePrice","lpCount" from public."Vault";' \
  -c 'select kind,assets from public."VaultFlow";' \
  -c 'select count(*) from public."Market";' \
  -c 'select id,"publicationCount" from public."IndexFeed" order by id;' \
  -c 'select count(*) from public."KuruTrade";'
```

Everything stored is WAD, widened once in `src/scale.ts`: a 250,000 USDC seed
deposit whose raw log says `250000000000` (6dp) reads back as
`250000000000000000000000`. Zero rows means the bindings are wrong — run the
drift test.

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

## 6. Kuru signatures

The OrderBook has no Solidity source in this repo, so the drift test cannot
check it — `UNCHECKED` in the test names it explicitly so a second unguarded
contract fails the suite. Signatures come from the published implementation ABI
and are re-verified with:

```sh
curl -s https://raw.githubusercontent.com/Kuru-Labs/kuru-sdk/main/abi/OrderBook.json \
 | python3 -c 'import json,sys;[print(e["name"]+"("+", ".join(i["type"]+(" indexed" if i.get("indexed") else "")+" "+i["name"] for i in e["inputs"])+")") for e in json.load(sys.stdin)["abi"] if e["type"]=="event"]'
```

Three of them are indexed: `OrderCreated`, `Trade`, `OrdersCanceled` (plural,
takes `uint40[]`). Markets are ERC-1967 proxies — the events are the
implementation's, the address is the proxy's, which is what `KURU_MARKETS`
holds. Price and size stay in the market's raw `uint32`/`uint96` tick units:
per-market `pricePrecision`/`sizePrecision` appears in no event, so scaling in
the indexer would be a guess. Consumers scale.
