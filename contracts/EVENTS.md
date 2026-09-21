# sweem contract events

Event reference for the Envio HyperIndex handlers. Every state change in the
protocol emits exactly one of these. Index **finalized blocks only** (Monad D=3,
finality at D+2) — the indexer is the historical surface, `reconciler` owns live state.

## Conventions

- All monetary and rate values are **1e18 fixed point ("wad")** unless the field name
  ends in `Assets`, which means **6-decimal USDC**.
- `marketId` on engine/registry events is the registry key: `keccak256(<market symbol>)`,
  e.g. `keccak256("VOL-ETH-USD")`. This is **not** the oracle's `feedId` — see IndexOracle.
- `roundId` is `uint64`.
- `size` is signed: positive is long, negative is short. `notional = |size| * index / 1e18`.
- Rates (`borrowRatePerSec`, `fundingRatePerSec`, `powerFundingPerSec`) are **per second**.
  Annualize with `* 31_536_000` — the exact constant `Accrual.SECONDS_PER_YEAR` and the
  oracle service both use.
- Cumulative indices (`borrowCum`, `fundingCum`) are integrals of those rates per unit
  notional. A position's owed amount is `entryNotional * (cumNow - cumEntry) / 1e18`.
- Positive `fundingRatePerSec` / `fundingCum` means **longs pay shorts**.
- AccessControl (`RoleGranted`/`RoleRevoked`/`RoleAdminChanged`) and ERC-20
  (`Transfer`/`Approval`) events are emitted but not listed here.

---

## IndexPerpEngine

### `PositionOpened(bytes32 indexed marketId, address indexed trader, int256 size, uint256 collateral, uint256 entryIndex, uint256 notional, uint256 fee)`
`0x83aa90aa8b2eb005c460d205e85e17a237feafd57443fb8a4ac6190bcd215df8`

A new position. `collateral` is post-fee. `fee` is the open fee, paid to the vault.

### `PositionModified(bytes32 indexed marketId, address indexed trader, int256 sizeDelta, int256 newSize, int256 collateralDelta, uint256 newCollateral, uint256 entryIndex, uint256 notional, uint256 fee)`
`0xb997c2728eabc2bb9c1532019045463e2c6975ccd081a7e4a38d190d0d758d14`

Size and/or collateral change. `collateralDelta` is **signed, in 6-decimal USDC**
(positive = deposit, negative = withdrawal). Always preceded by a `PositionSettled`
in the same transaction. `entryIndex` is the *re-snapshotted* entry.

### `PositionClosed(bytes32 indexed marketId, address indexed trader, int256 size, uint256 exitIndex, int256 realizedPnl, uint256 payout, uint256 fee)`
`0xa6ba49088891a91b5a91941a8bbb8f9ffa707350ce441c2ca7b4fe6397dbc1aa`

`size` is the size that was closed. `payout` is **6-decimal USDC** sent to the trader.
`realizedPnl` is price PnL only — borrow and funding are in the paired `PositionSettled`.

### `PositionLiquidated(bytes32 indexed marketId, address indexed trader, address indexed liquidator, int256 size, uint256 exitIndex, uint256 penalty, uint256 liquidatorReward, uint256 traderRefund)`
`0xc6568eca4075e6e9ad4b125bec641464d10078a30a75496227b7f876b49f6002`

`penalty` is wad; `liquidatorReward` is wad (half the penalty, the vault keeps the rest);
`traderRefund` is **6-decimal USDC**.

### `PositionSettled(bytes32 indexed marketId, address indexed trader, int256 pnl, uint256 borrowPaid, int256 fundingPaid, uint256 badDebt, uint256 newCollateral)`
`0x3f06ca9bf8e123709e99290f0292a44e256990fdbe5df5ca80a6f5967070799f`

**The realization event — the one to build LP and trader PnL curves from.** Emitted on
every modify, close and liquidate, before the action's own event. `fundingPaid` is signed
from the trader's perspective (positive = the trader paid). `badDebt > 0` means the loss
exceeded collateral and LPs absorbed the remainder.

### `AccrualUpdated(bytes32 indexed marketId, uint256 dt, uint256 borrowRatePerSec, int256 fundingRatePerSec, int256 powerFundingPerSec, uint256 borrowCum, int256 fundingCum, uint256 markPrice, uint256 indexPrice)`
`0xd716d2729a6250c815925d7e71b0bf0f0c278f62519ef9bea6e80ac16236e6eb`

Market-level accrual tick. `dt` is elapsed seconds since the previous accrual.
`powerFundingPerSec` is the `k(k-1)/2 * sigma^2` coupling term drawn from the vol index
oracle (0 for non-power markets); `fundingRatePerSec` is the **total** applied rate
(skew + power, clamped to the market's cap). `markPrice` is derived from vault skew:
`index * (1 + (longOI - shortOI) / skewScale)`. This is the funding-history series.

### `OpenInterestChanged(bytes32 indexed marketId, uint256 longOI, uint256 shortOI, uint256 totalOpenNotional)`
`0x395c4aecfd49f563cd34655f3584520723b6729c5eaf26390cbf0d5c71b6b98f`

Per-market OI plus the protocol-wide total. Skew is `longOI - shortOI`.

### `MarketHalted(bytes32 indexed marketId, address indexed guardian, bool halted)`
`0x79633ec16a15a6d31c36ce3cad3d0b2999ba51219073d4716103579697a62165`

### `Paused(address account)` / `Unpaused(address account)`
`0x62e78cea01bee320cd4e420270b5ea74000d11b0c9f74754ebdbfc544b05a258` /
`0x5db9ee0a495bf2e6ff9c91a7834c1ba4fdd244a5e8aa4e537bd38aeae4b073aa`

Global kill switch. Also emitted by `SweemVault` — key handlers by contract address.

---

## SweemVault

### `Deposit(address indexed sender, address indexed owner, uint256 assets, uint256 shares)`
`0xdcbc1c05240f31ff3ad067ef1ee35ce4997762752e3a095284754544f4c709d7`

### `Withdraw(address indexed sender, address indexed receiver, address indexed owner, uint256 assets, uint256 shares)`
`0xfbde797d201c681b91056529119e0b02407c7bb96a4a2c75c01fc9667232c8db`

Standard ERC-4626. `assets` is **6-decimal USDC**; `shares` is 18-decimal `sUSDC`.
LP PnL is `assets` out vs `assets` in per share, or track `totalAssets/totalSupply`.

### `EnginePaidOut(address indexed engine, address indexed to, uint256 assets, uint256 amountWad)`
`0x274a0d708abe9d7b042356b07d13c291f0ccb80c907fdd0c270e35d5a7f54d9e`

LP capital leaving the vault to cover trader profit. `assets` is 6-decimal, `amountWad` 1e18.
There is no matching "paid in" event — inflows arrive as a plain ERC-20 `Transfer` to the
vault; derive vault balance from `Transfer` logs or from `totalAssets()` at the block.

### `OpenNotionalSynced(uint256 openNotional, uint256 utilization)`
`0xdbfa5c6c2dc9f115263eab2d04774677cc29a50c52812eb1e94b4ddd601d9de6`

Emitted on every OI change. `utilization = openNotional / TVL`, 1e18. This is the input to
`borrowRate = baseRate + slope * utilization`. Utilization time series comes from here.

### `BadDebtRecorded(address indexed engine, uint256 amountWad, uint256 totalBadDebt)`
`0xf824141b5865c6fe993e72651d58296151858045f63dc2bf611ba62429e3d1d6`

### `VaultParamsUpdated(uint256 tvlCap, uint256 maxUtilization, uint256 baseRatePerSecond, uint256 slopePerSecond)`
`0x6ec5a67a668f5bcbf15f3a8e1083d1cf68729a0e379ae85701121a0fce41dda9`

`tvlCap` is 6-decimal USDC; the rest are 1e18 per second.

---

## IndexOracle

**One instance per feed.** Each instance exposes `feedId()` — `keccak256(bytes(marketId))`
where `marketId` is the oracle service's canonical string, e.g. `sweem:vol:ETH/USD`. Index
by contract address; `feedId`/`marketId` are how you label the series. The vol perp's index
oracle is also the sigma feed the power markets couple their funding to — same contract,
two consumers.

Two write paths exist and are **indistinguishable by design**: `onReport(bytes,bytes)`
(the Chainlink keystone forwarder, report = `abi.encode(feedId, value)`) and
`pushIndex(bytes32,uint256)` (the hot-key fallback). Both hold the same `WRITER_ROLE` and
run the same validation, so the events below are identical whichever path produced them.
The `writer` field on `IndexPublished` is the only way to tell, and it is informational.

### `IndexPublished(uint64 indexed roundId, uint256 value, uint256 timestamp, address indexed writer)`
`0x9389c514850e787c2d428dde7775a667832d5c86434334b5d20b0a730914afa2`

The index time series. `writer` is the forwarder address when the publication came through
CRE and the hot key when it came through the fallback.

### `BreakerTripped(uint64 indexed roundId, uint256 lastValue, uint256 rejectedValue, uint256 deviationBps)`
`0x36260666b5d8a2f9a20e4da5bbd01dbc132768f9f50deb902971c773a70ed9e1`

The update exceeded `maxDeviationBps` and was **rejected** — `rejectedValue` was never
written. The oracle is now halted and every market reading it reverts.

### `BreakerReset(address indexed admin, uint256 value, uint256 timestamp)`
`0xed2b068360b8e712676ab6a500b8d7eb2df7d03e003c4291f53fe515d888d411`

Breaker cleared and a fresh value seeded. Bumps `roundId` without an `IndexPublished`.
A tripped breaker does **not** bump `roundId` and does **not** write the value.

### `OracleParamsUpdated(uint256 maxStaleness, uint256 maxDeviationBps)`
`0x623750128e1cd2cdf68fb11d58d77e951b929f0cb6918721355fd7169df216bc`

Staleness is *not* eventful — it is a read-time condition. Detect it by comparing
`IndexPublished.timestamp` against `maxStaleness`.

---

## MarketRegistry

### `MarketAdded(bytes32 indexed marketId, address indexed indexOracle, address indexed volOracle, int256 k)`
`0x4eb03b50a32fbe02208e68d88f5d1448b7218fe88b61013c7a30ffb6d7fb795e`

`k == 0` is a plain index perp (the vol perp). `k != 0` is a power perp on `(P/P0)^k`,
and `volOracle` is the sigma feed driving its `k(k-1)/2 * sigma^2` funding term.

### `MarketParamsUpdated(bytes32 indexed marketId, (address,address,int256,uint256,uint256,uint256,uint256,uint256,uint256,uint256,uint256,uint256,uint256,bool) params)`
`0x7c6bc8cc27b9df8670af39761576141cf6dee9041fd5adf2caf7701e3bc261e3`

Tuple order: `indexOracle, volOracle, k, maxOiPerSide, maxLeverage, minPositionSize,
maintenanceMarginBps, openFeeBps, closeFeeBps, liquidationFeeBps, fundingK,
fundingCapPerSec, skewScale, enabled`. Also emitted on `addMarket`.

### `MarketEnabled(bytes32 indexed marketId, bool enabled)`
`0x8859cebcc73558fc4daa8565f5a9f47736c21833eccd85b53cba0c35f5fb5c3e`

---

## Ordering within a transaction

Open:      `AccrualUpdated?` → `OpenInterestChanged` → `PositionOpened`
Modify:    `AccrualUpdated?` → `PositionSettled` → `OpenInterestChanged` (×2) → `PositionModified`
Close:     `AccrualUpdated?` → `PositionSettled` → `OpenInterestChanged` → `PositionClosed`
Liquidate: same as close, ending in `PositionLiquidated`

`AccrualUpdated` is skipped when no time has elapsed since the last accrual.
`OpenNotionalSynced` accompanies every `OpenInterestChanged`.
