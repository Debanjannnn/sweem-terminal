# Perpl risk + analytics, visible in UI

Goal: show judges (Perpl "Best Analytics / Risk Tool") that Perpl's API + contract drive risk and analytics. Three surfaces.

Data sources:
- Perpl REST (already in publisher rates stream): live funding, 7d short APR
- Perpl REST history (already in history `series/funding`): daily funding, 30d chart
- Perpl exchange contract views (new for API): getPerpetualInfo (mark/oracle/OI), getMarginFractions (init/maint), getAccountByAddr + getPosition (live short)

## Steps
- [x] 1. `internal/perpl` (shared): move ABI, `Perp`, position read, `Guard`, `Gate` out of scheduler; add `Market` read + `LiqPrice`. Scheduler imports it (no behavior change).
- [x] 2. API
  - `GET /api/v1/perpl/markets`: per perp (ETH/BTC/MON) mark, oracle, maint/init margin, max lev, OI, live + 7d funding APR; plus guardBuffer, gateBand
  - `GET /api/v1/me/positions/{id}/perpl`: live short (size, entry, collateral, PnL, margin frac vs maint, liq price, guard price), guard action, gate APR vs threshold + state, next bot action
  - env `GUARD_BUFFER`, `FUNDING_GATE_BAND` (same defaults as scheduler, shared consts)
- [x] 3. Client
  - Earn page: "Perpl funding" strip (3 markets, live + 7d APR, 30d sparkline)
  - Builder (basis on): "Perpl risk" card: funding now/7d vs gate, 30d chart, maint margin, liq + guard price at chosen leverage, guard steps
  - Portfolio position: live risk panel (margin gauge, liq distance, funding PnL, gate state, next action)
- [x] 4. Verify: go build/vet/test, curl endpoints on local stack, tsc/eslint/build, screenshots

## Review (2026-10-13)
- `internal/perpl`: ABI, `Perp` (+ entry, funding = premiumPnlCNS, verified pnl = delta + premium on mainnet), `Guard`, `Gate`, `LiqPrice`, `Market`. Scheduler imports it, behavior unchanged, tests moved.
- API: `GET /perpl/markets` (live on local stack), `GET /me/positions/{id}/perpl` (live test vs real mainnet ETH short, acct 533: margin 22%, liq 2907.51, guard 2826.74; hand-checked).
- Client: earn funding strip, builder risk card (gate state uses scheduler's exact input: avg of last N daily APRs), portfolio risk panel (previewed with live acct 533 data, healthy + guard-fire).
- Verified: go build/vet (all tags)/test, tsc, eslint, next build, screenshots.
- Finding: ETH 7d daily avg 4.69% < default 5% gate → a fresh ETH basis deploy gets parked by the next scheduler pass. Demo: gate ≤ 4% or off.
- Not done: position panel never seen with a real logged-in position (needs user's deploy).
