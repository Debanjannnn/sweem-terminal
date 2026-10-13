# Baskets = earn strategies (one product)

A basket is what the strategy builder makes today: tokens × yield sources, deployed into the user's wallet. Strategy model unchanged. The old spot-basket feature is deleted. UI says "Basket" everywhere.

## Backend
- [x] 1. Withdraw takes optional `bps` (partial sell, planner WithdrawBps).
- [x] 2. Recurring buy: schedule kind `dca` on target `strategy` → scheduler deploys a new position each interval (same path as API deploy: fee, plan, OpenPosition).
- [x] 3. Delete old baskets: API routes/handlers/store (basket_defs, basket_buy/sell, BasketHolding), planner BasketBuy/BasketSell/ValidateLegs callers, scheduler basket targets, market-data basket quote, e2e/tests. Migration drops basket_defs.
## Client
- [x] 4. Routes: `/baskets` list, `/baskets/new` builder, `/baskets/[id]` detail; `/earn*` → redirect.
- [x] 5. Copy: Earn/Strategy → Basket (nav, home, search, builder, detail, deploy, portfolio "My baskets").
- [x] 6. Remove CreateBasket, BasketTrade, old basket page/index/backtest bits; keep shared bits (IconStack, COLORS).
- [x] 7. Detail page: partial withdraw (%), recurring buy form.
- [x] 8. Verify: go test/vet (all tags), cargo test, tsc/eslint/build, local stack, screenshots.

## Review (2026-10-13)
- Backend: partial withdraw (`bps`, PartialWithdraw store: open + nothing in flight, legs via reconcile); recurring buy = dca on `strategy` target → scheduler `recurringBuy` opens position + deploy exec + fee row in one tx (mirrors api OpenPosition), settles opening→open/failed; basket API/store/planner (BasketBuy/Sell, ValidateLegs, AttributeFills)/scheduler targets/market-data /quote/basket removed.
- Migration 0014: legacy basket/null schedules deactivated + target_type null (they have execution history; delete hit FK on real local data), target_type check → market|position|strategy, drop basket_id cols + basket_defs. Schedules API hides null-target rows.
- Client (subagent + fixes): /baskets list/new/[id], /earn/* 308 → /baskets; copy Basket everywhere; old basket UI deleted; portfolio sell 25/50/75/100%, withdrawing → "Resume sell"; recurring buy card on detail; HODL stat removed.
- Verified: go build/vet (all tags), go test unit + integration (api, scheduler incl. TestRecurringBuy fee 0.4/owed 0.2/open, pg migrations, market-data), cargo test + pg integration, tsc/eslint/next build, local stack migrated + smoke (old routes 404/405), screenshots list + detail.
- Not verified: logged-in flows (sell %, recurring buy form, portfolio) visually; real-money partial withdraw / recurring deploy.
