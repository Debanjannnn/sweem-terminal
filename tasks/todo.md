# AUSD-quoted Kuru markets

Goal: trade MON/AUSD and AUSD/USDC on Kuru next to existing USDC books. Spot trading only; baskets, strategies, fees stay USDC.

Key decision: keep existing keys (MON, WETH, cbBTC, XAUt0) = USDC books. Add new keys `MON_AUSD`, `AUSD_USDC` (Kuru's own symbols). Everything that looks up by asset keeps working; only quote-is-USDC assumptions change.

Live params (exchangeInfo):
- MON_AUSD `0x131a2e70a5b31a517a74b8c567149bc294470da9` pricePrec 1e8, sizePrec 1e10, tick 1, minSize 2e12
- AUSD_USDC `0x699abc15308156e9a3ab89ec7387e9cfe1c86a3b` pricePrec 1e8, sizePrec 1e6, tick 1, minSize 1e7
- WBTC_AUSD skipped: empty book, WBTC not a supported token

## Steps
- [x] 1. Config: add both markets to `contracts/deployments/143.json` + `client/src/abi/deployments.json`
- [x] 2. Executor policy: no code (rules iterate orderbooks + tokens). Run `policy_rules` test, then re-apply live Privy policy (outward action, confirm first)
- [x] 3. Publisher: none (resolves symbol by orderbook); verify startup Resolve passes
- [x] 4. Indexer: none (collects all orderbooks)
- [x] 5. (moot: baskets restricted to USDC books, so no double count) API holdings: dedupe by base token so MON_AUSD + MON don't double count (`trade.go:194,517`, `api.go:320`, `store.go:130`)
- [x] 6. Scheduler TP/SL on AUSD market: price is AUSD/unit, treat AUSD≈USD (comment), no conversion
- [x] 7. History ingest: skip new markets (backtests use asset → USDC book)
- [x] 8. Client
  - `tokens.ts`: `MarketDef.quote`; `marketOf` keyed by market key
  - trade page caption, OrderBook, LimitPanel, MarketPanel `inTok/outTok` from `market.quote` (real bug otherwise)
  - home list / search: show pair ("MON / AUSD"), not just token name
  - baskets + strategies: filter to USDC-quoted markets only
  - ScheduleForm price labels from quote
- [ ] 9. Verify (all but real-money trade done): go test ./..., cargo test (executor, indexer), tsc/eslint/build, e2e buy+sell tiny MON_AUSD on mainnet via UI

## Unresolved questions
1. Uncommitted backend work in same files (planner, api, scheduler). Build on it, or wait till committed?
2. AUSD_USDC overlaps 3pool swap. List it on trade page anyway?
3. OK to re-apply live Privy policy after deploy?
4. MON_AUSD book thin (39bps spread). Show warning on its trade page?

## Review (2026-10-13)
- Done: config (6 markets), planner cap mirror for stable-base sells, baskets USDC-only, scheduler gas reserve by base, client quote-aware trade/limit/book/automation + quote selector + thin-book notice, lists one row per token.
- Found + fixed: quiet books never republished → market-data 30s staleness refused AUSD quotes. Publisher now republishes idle books every 10s.
- Live Privy policy PATCHed (25 rules, Kuru rules cover 6 books). Second verify call blocked by permission; verify in Privy dashboard.
- MON/AUSD book ~18% below MON/USDC, ~250 MON bid depth: effectively untradable today.
- Not done: real-money MON_AUSD trade (needs user wallet signature).
- Basket page: Market tab, spend→receive card like coin page, value on chart, backtest card (new history "hold" leg).
