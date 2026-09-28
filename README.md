# sweem baskets

Tokenized crypto baskets on Monad. Deposit USDC, the basket contract market-buys
its components (MON / WETH / cbBTC) on Kuru, and you get ERC-20 shares back.
Redeem sells at NAV. A protected variant also holds a Perpl short as a synthetic put.

**Make your money sweem.**

> Mid-pivot from sweem-terminal. The services below are kept infra; the basket
> contract, keepers and basket UI are not written yet.

---

## Architecture

```
                              browser
                                 |
                    Privy embedded wallet (yours)
                                 |  access token (ES256)
                                 v
  +------------------------------------------------------------------+
  | client        Next.js :3000   Privy shell, portfolio              |
  +------------------------------------------------------------------+
        |  HTTP, Bearer JWT                    |  WS, unauthenticated
        v                                      v
  +------------------------+        +--------------------------------+
  | wallet    Go :8080     |        | market-data   Go :8081 / ws 8082|
  | auth, delegation,      |<-------| kuru + perpl ingest, ticks,     |
  | policy, intents,       |  read  | candles, L2 book, quotes        |
  | portfolio              |        +--------------------------------+
  | ONLY client of executor|                   ^        |
  +-----------+------------+                   |        | redis / nats
        |     | postgres                       |        v
        |     v                          +-----+----+  +------------+
        |  users, delegations,           | indexer  |  | reconciler |
        |  intents, executions           | envio    |  | Go :8085   |
        |                                | finalized|  | D+2 truth  |
        | POST /route                    +----------+  +------------+
        v                                                     |
  +------------------------+                                  v
  | executor   Rust :8083  |                          execution.settled
  | NO HOST PORT           |
  | the ONLY thing         |
  | that signs             |
  +-----------+------------+
        |
        v
   Privy -> your wallet -> Kuru / Perpl
```

Two invariants the whole design rests on:

> **Reconciler detects, never signs. Executor signs, never decides.**

> **Signing authority lives in exactly one process.**

Any automated caller (a keeper) is a *trigger*, not an authorization. It can be crashed,
restarted, or accidentally run twice without risking funds, because `wallet` re-checks
delegation, caps, policy and balance before the executor is ever called.

| service | lang | port | does | never does |
|---|---|---|---|---|
| `client` | TS/Next 15 | 3000 | render, hold the Privy session, grant/revoke delegation | talk to the executor |
| `wallet` | Go | 8080 | auth, delegation, policy, intents, portfolio | sign, derive market data |
| `executor` | Rust | 8083 internal | build calldata, sign via Privy, submit | decide *whether* to trade |
| `market-data` | Go | 8081/8082 | venue ingest, candles, book, browser fanout | sign, write chain |
| `reconciler` | Go | 8085 | chain truth, D+2 finality, settle executions | sign, serve the browser |
| `oracle` | TS | — | library only: Pyth/Chainlink price reads + EWMA vol (prices protection) | run, publish |
| `indexer` | TS/Envio | 8090 | finalized events → GraphQL | read speculative state |

---

## Monad specifics that shape the product

`latest` = **Proposed — speculative, and can change on an identical retry.**
`safe` = Voted. `finalized` = Finalized. `eth_getTransactionByHash` returns **null** for
mempool-only transactions rather than reporting them pending.

So the UI shows three tx states — **seen · safe · final** — in the orders table, not a
binary spinner in a toast. `reconciler` compares only against `finalized` and refuses to
diff inside the D+2 window. `indexer` runs at `block_lag = 5` and never sees a block that
can change.

Monad also **charges the full gas limit up front** at a ~100 gwei base fee. Every
transaction the executor builds carries an explicit `gas_limit`; leaving it to a default
means a 21k transfer fails from a funded wallet.

---

## Running it

```sh
cp .env.example .env          # fill in the values marked required
make infra                    # postgres, redis, nats, migrations
make dev                      # every backend service, backgrounded
make client                   # http://localhost:3000
make status                   # /health on every port
```

`client` and `indexer` run outside compose on purpose: `NEXT_PUBLIC_*` bakes at build
time, so `bun dev` beats rebuilding an image per change.

Required before first run: `PRIVY_APP_ID`, `INTERNAL_SECRET`, and — in `.env.executor`,
which nothing else may read — `PRIVY_APP_SECRET` and `PRIVY_AUTHORIZATION_PRIVATE_KEY`.
`wallet` refuses to boot if it can see those. That guard is a feature.
`ENVIO_API_TOKEN` is required for the indexer; HyperSync has no unauthenticated mode.

```sh
make test     # go + cargo + forge (spikes excluded) + oracle math
make test-spikes  # contracts/test/spikes: live-fork Kuru + Perpl probes, slow
make check    # vet + clippy + client build
make routes   # replay every client-built URL against the owning service
```

---

## Repo layout

Every non-Go unit is a top-level directory that builds on its own. There is **no JS
monorepo** — `client/`, `oracle/` and `indexer/` are standalone **bun** projects with
their own lockfiles. The shared types a workspace would have carried come from `proto/`
instead, generated into each consumer and committed.

```
proto/            buf: routes, domain types, NATS events -> Go, Rust, TS
internal/shared/  Go packages every service imports
services/         wallet, market-data, reconciler
executor/         Rust, its own cargo workspace
contracts/        Foundry (src/ empty; test/spikes/ = Kuru + Perpl fork probes)
client/           Next.js       (bun)
oracle/           TS, EWMA vol math + price sources (bun)
indexer/          Envio         (bun)
infra/            terraform + ansible
```

`proto/` is not ceremony. The two worst bugs in the previous stack were both one service
calling a route another never registered, swallowed by a `catch { return null }`. A
TypeScript route table caught them then; this stack is Go + Rust + TS, so the contract
has to be language-neutral. `make routes` replays every URL a client builds against the
owning service's mux and fails on a miss.

---

## Status

Pivoting. Contracts for the old power/velocity perps, the scheduler (TWAP/DCA/grid)
and the oracle publisher are removed. Next: basket contract on Kuru mainnet v1
(`placeAndExecuteMarketBuy/Sell`), Perpl hedge, keepers, basket UI.
