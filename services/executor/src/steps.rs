//! adapters: one plan step → the one venue call that performs it. pure; every target comes from 143.json,
//! every receiver/onBehalfOf is the user's own wallet. amounts are token atoms unless the field says otherwise.
use alloy::{
    primitives::{aliases::{U40, U96}, Address, U256},
    sol_types::SolCall,
};
use serde::{Deserialize, Serialize};

use crate::{config::{Deployments, Market}, contracts::*};

#[derive(Debug, Clone, PartialEq, Deserialize, Serialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Hop {
    pub market: String,
    pub is_buy: bool,
}

#[derive(Debug, Clone, PartialEq, Deserialize, Serialize)]
#[serde(tag = "action", rename_all_fields = "camelCase", deny_unknown_fields)]
pub enum Step {
    /// quote: quote-token atoms spent; minOut: base atoms
    #[serde(rename = "kuru.marketBuy")]
    KuruMarketBuy { market: String, quote: String, min_out: String },
    /// size: base atoms sold (floored to sizePrecision); minOut: quote atoms
    #[serde(rename = "kuru.marketSell")]
    KuruMarketSell { market: String, size: String, min_out: String },
    #[serde(rename = "kuru.marginDeposit")]
    KuruMarginDeposit { token: String, amount: String },
    #[serde(rename = "kuru.marginWithdraw")]
    KuruMarginWithdraw { token: String, amount: String },
    /// price/size raw in the market's pricePrecision/sizePrecision; funds come from the margin account
    #[serde(rename = "kuru.limit")]
    KuruLimit { market: String, side: String, price: String, size: String, post_only: bool },
    #[serde(rename = "kuru.cancel")]
    KuruCancel { market: String, order_ids: Vec<String> },
    #[serde(rename = "kuru.swap")]
    KuruSwap { hops: Vec<Hop>, from: String, to: String, amount: String, min_out: String },
    /// venue: aaveV3 | neverland
    #[serde(rename = "lend.supply")]
    LendSupply { venue: String, asset: String, amount: String },
    /// amount "max" withdraws everything
    #[serde(rename = "lend.withdraw")]
    LendWithdraw { venue: String, asset: String, amount: String },
    #[serde(rename = "vault.deposit")]
    VaultDeposit { vault: Address, amount: String },
    #[serde(rename = "vault.redeem")]
    VaultRedeem { vault: Address, shares: String },
    #[serde(rename = "vault.withdraw")]
    VaultWithdraw { vault: Address, amount: String },
    #[serde(rename = "shmonad.deposit")]
    ShmonadDeposit { amount: String },
    #[serde(rename = "shmonad.redeem")]
    ShmonadRedeem { shares: String },
    /// base: MON wei (sent as value); quote: USDC atoms
    #[serde(rename = "activeVault.deposit")]
    ActiveVaultDeposit { base: String, quote: String },
    #[serde(rename = "activeVault.withdraw")]
    ActiveVaultWithdraw { shares: String },
    /// AUSD/USDC/USDT0 3pool
    #[serde(rename = "stable.swap")]
    StableSwap { from: String, to: String, amount: String, min_out: String },
    /// AUSD atoms
    #[serde(rename = "perpl.createAccount")]
    PerplCreateAccount { amount: String },
    #[serde(rename = "perpl.deposit")]
    PerplDeposit { amount: String },
    /// kind openShort|closeShort, IOC; price/lot raw in the perp's price/size decimals
    #[serde(rename = "perpl.order")]
    /// kind addMargin {perp, amount}: isolated margin, moves `amount` AUSD atoms of free account balance into the position
    PerplOrder { perp: String, kind: String, price: Option<String>, lot: Option<String>, leverage_hdths: Option<u64>, amount: Option<String> },
    /// asks Perpl to release position margin to free balance; completed or declined later by Perpl, not in this tx
    #[serde(rename = "perpl.requestRemoveMargin")]
    PerplRequestRemoveMargin { perp: String, amount: String },
    #[serde(rename = "perpl.withdraw")]
    PerplWithdraw { amount: String },
    /// MON wei → WMON (value)
    #[serde(rename = "wmon.wrap")]
    WmonWrap { amount: String },
    #[serde(rename = "wmon.unwrap")]
    WmonUnwrap { amount: String },
    /// USDC atoms to the treasury (strategy open fee)
    #[serde(rename = "fee.transfer")]
    FeeTransfer { amount: String },
}

/// one tx. `spend` = token the call takes from the wallet; `approve` = `to` pulls it, so it needs allowance first.
#[derive(Debug, Clone, PartialEq)]
pub struct Call {
    pub to: Address,
    pub value: U256,
    pub data: Vec<u8>,
    pub spend: Option<(Address, U256)>,
    pub approve: bool,
}

pub const STABLES: [&str; 3] = ["USDC", "AUSD", "USDT0"];

fn amt(s: &str) -> Result<U256, String> {
    let a = u256_str(s)?;
    if a.is_zero() { Err("zero amount".into()) } else { Ok(a) }
}

fn token(d: &Deployments, sym: &str) -> Result<(Address, u8), String> {
    d.tokens.get(sym).map(|t| (t.address, t.decimals)).ok_or_else(|| format!("unknown token {sym}"))
}

fn market<'a>(d: &'a Deployments, sym: &str) -> Result<&'a Market, String> {
    d.kuru.markets.get(sym).ok_or_else(|| format!("unknown market {sym}"))
}

fn u96(v: U256) -> Result<U96, String> {
    if v.is_zero() { return Err("rounds to zero".into()) }
    if v >= U256::from(1u128 << 96) { return Err(format!("{v} overflows uint96")) }
    let l = v.as_limbs();
    Ok(U96::from_limbs([l[0], l[1]]))
}

fn pow10(d: u8) -> U256 {
    U256::from(10).pow(U256::from(d))
}

/// pulls `amount` of `tok` from the wallet: native goes as value, ERC-20 needs an approve.
fn call(to: Address, data: Vec<u8>, tok: Address, amount: U256) -> Call {
    let native = tok.is_zero();
    Call { to, value: if native { amount } else { U256::ZERO }, data, spend: Some((tok, amount)), approve: !native }
}

fn plain(to: Address, data: Vec<u8>) -> Call {
    Call { to, value: U256::ZERO, data, spend: None, approve: false }
}

pub fn build(d: &Deployments, wallet: Address, step: &Step) -> Result<Call, String> {
    use Step::*;
    Ok(match step {
        KuruMarketBuy { market: m, quote, min_out } => {
            let m = market(d, m)?;
            let (q, qdec) = token(d, &m.quote)?;
            let quote = amt(quote)?;
            let quote_size = u96(quote * U256::from(m.price_precision) / pow10(qdec))?;
            let data = IOrderBook::placeAndExecuteMarketBuyCall { quoteSize: quote_size, minAmountOut: u256_str(min_out)?, isMargin: false, isFillOrKill: false };
            call(m.orderbook, data.abi_encode(), q, quote)
        }
        KuruMarketSell { market: m, size, min_out } => {
            let m = market(d, m)?;
            let (b, bdec) = token(d, &m.base)?;
            let size = u96(amt(size)? * U256::from(m.size_precision) / pow10(bdec))?;
            let used = U256::from(size) * pow10(bdec) / U256::from(m.size_precision);
            let data = IOrderBook::placeAndExecuteMarketSellCall { size, minAmountOut: u256_str(min_out)?, isMargin: false, isFillOrKill: false };
            call(m.orderbook, data.abi_encode(), b, used)
        }
        KuruMarginDeposit { token: t, amount } => {
            let (t, _) = token(d, t)?;
            let a = amt(amount)?;
            call(d.kuru.margin_account, IMarginAccount::depositCall { user: wallet, token: t, amount: a }.abi_encode(), t, a)
        }
        KuruMarginWithdraw { token: t, amount } => {
            let data = IMarginAccount::withdrawCall { amount: amt(amount)?, token: token(d, t)?.0 };
            plain(d.kuru.margin_account, data.abi_encode())
        }
        KuruLimit { market: m, side, price, size, post_only } => {
            let ob = market(d, m)?.orderbook;
            let price: u32 = price.parse().ok().filter(|p| *p > 0).ok_or("price is not a positive uint32")?;
            let size = u96(u256_str(size)?)?;
            let data = match side.as_str() {
                "buy" => IOrderBook::addBuyOrderCall { price, size, postOnly: *post_only }.abi_encode(),
                "sell" => IOrderBook::addSellOrderCall { price, size, postOnly: *post_only }.abi_encode(),
                s => return Err(format!("side {s}")),
            };
            plain(ob, data)
        }
        KuruCancel { market: m, order_ids } => {
            let ids = order_ids.iter().map(|i| i.parse::<U40>().map_err(|_| format!("order id {i}"))).collect::<Result<Vec<_>, _>>()?;
            if ids.is_empty() {
                return Err("no order ids".into());
            }
            plain(market(d, m)?.orderbook, IOrderBook::batchCancelOrdersCall { orderIds: ids }.abi_encode())
        }
        KuruSwap { hops, from, to, amount, min_out } => {
            let markets = hops.iter().map(|h| market(d, &h.market).map(|m| m.orderbook)).collect::<Result<Vec<_>, _>>()?;
            if markets.is_empty() {
                return Err("no hops".into());
            }
            let (debit, credit, a) = (token(d, from)?.0, token(d, to)?.0, amt(amount)?);
            let data = IRouter::anyToAnySwapCall {
                markets,
                isBuy: hops.iter().map(|h| h.is_buy).collect(),
                nativeSend: (0..hops.len()).map(|i| i == 0 && debit.is_zero()).collect(),
                debitToken: debit,
                creditToken: credit,
                amount: a,
                minAmountOut: u256_str(min_out)?,
            };
            call(d.kuru.router, data.abi_encode(), debit, a)
        }
        LendSupply { venue, asset, amount } => {
            let (pool, (t, _), a) = (lend_pool(d, venue)?, token(d, asset)?, amt(amount)?);
            call(pool, IPool::supplyCall { asset: t, amount: a, onBehalfOf: wallet, referralCode: 0 }.abi_encode(), erc20(t)?, a)
        }
        LendWithdraw { venue, asset, amount } => {
            let a = if amount == "max" { U256::MAX } else { amt(amount)? };
            plain(lend_pool(d, venue)?, IPool::withdrawCall { asset: erc20(token(d, asset)?.0)?, amount: a, to: wallet }.abi_encode())
        }
        VaultDeposit { vault, amount } => {
            let (asset, a) = (token(d, &vault_asset(d, *vault)?)?.0, amt(amount)?);
            call(*vault, IVault::depositCall { assets: a, receiver: wallet }.abi_encode(), erc20(asset)?, a)
        }
        VaultRedeem { vault, shares } => {
            vault_asset(d, *vault)?;
            plain(*vault, IVault::redeemCall { shares: amt(shares)?, receiver: wallet, owner: wallet }.abi_encode())
        }
        VaultWithdraw { vault, amount } => {
            vault_asset(d, *vault)?;
            plain(*vault, IVault::withdrawCall { assets: amt(amount)?, receiver: wallet, owner: wallet }.abi_encode())
        }
        ShmonadDeposit { amount } => {
            let a = amt(amount)?;
            call(d.staking.shmonad, IVault::depositCall { assets: a, receiver: wallet }.abi_encode(), Address::ZERO, a)
        }
        ShmonadRedeem { shares } => plain(d.staking.shmonad, IVault::redeemCall { shares: amt(shares)?, receiver: wallet, owner: wallet }.abi_encode()),
        ActiveVaultDeposit { base, quote } => {
            let (base, quote) = (amt(base)?, amt(quote)?);
            let data = IActiveVault::depositCall { baseAmount: base, quoteAmount: quote }.abi_encode();
            Call { to: d.kuru.active_vault, value: base, data, spend: Some((token(d, "USDC")?.0, quote)), approve: true }
        }
        ActiveVaultWithdraw { shares } => plain(d.kuru.active_vault, IActiveVault::withdrawCall { shares: amt(shares)? }.abi_encode()),
        StableSwap { from, to, amount, min_out } => {
            let p = &d.swap.ausd3pool;
            let idx = |s: &str| p.coins.get(s).copied().ok_or_else(|| format!("{s} not in 3pool"));
            let a = amt(amount)?;
            let data = IStableSwap::exchangeCall { i: idx(from)?, j: idx(to)?, dx: a, min_dy: u256_str(min_out)? };
            call(p.pool, data.abi_encode(), token(d, from)?.0, a)
        }
        PerplCreateAccount { amount } => {
            let a = amt(amount)?;
            call(d.perpl.exchange, IPerpl::createAccountCall { amountCNS: a }.abi_encode(), token(d, "AUSD")?.0, a)
        }
        PerplDeposit { amount } => {
            let a = amt(amount)?;
            call(d.perpl.exchange, IPerpl::depositCollateralCall { amountCNS: a }.abi_encode(), token(d, "AUSD")?.0, a)
        }
        PerplOrder { perp, kind, price, lot, leverage_hdths, amount } => {
            let none = |o: &Option<String>| o.is_none();
            // (type, price, lot, leverage, IOC, amount, maxNegPnl); addMargin as verified on a fork: over-asking reverts
            let (t, price, lot, lev, ioc, amount, neg) = match (kind.as_str(), price, lot, leverage_hdths, amount) {
                (k @ ("openShort" | "closeShort"), Some(p), Some(l), Some(lev), None) => {
                    (if k == "openShort" { 1 } else { 3 }, amt(p)?, amt(l)?, *lev, true, U256::ZERO, 1000)
                }
                ("addMargin", p, l, None, Some(a)) if none(p) && none(l) => (5, U256::ZERO, U256::ZERO, 100, false, amt(a)?, 0),
                (k, ..) => return Err(format!("perpl.order kind {k}: bad or missing fields")),
            };
            let desc = IPerpl::OrderDesc {
                orderDescId: U256::ZERO,
                perpId: U256::from(perp_id(d, perp)?),
                orderType: t,
                orderId: U256::ZERO,
                pricePNS: price,
                lotLNS: lot,
                expiryBlock: U256::ZERO,
                postOnly: false,
                fillOrKill: false,
                immediateOrCancel: ioc,
                maxMatches: U256::ZERO,
                leverageHdths: U256::from(lev),
                lastExecutionBlock: U256::ZERO,
                amountCNS: amount,
                maxNegPnlCollatBPS: U256::from(neg),
            };
            plain(d.perpl.exchange, IPerpl::execOrderCall { orderDesc: desc }.abi_encode())
        }
        PerplRequestRemoveMargin { perp, amount } => {
            let data = IPerpl::requestDecreasePositionCollateralCall { perpId: U256::from(perp_id(d, perp)?), amountCNS: amt(amount)?, clampToMaximum: false };
            plain(d.perpl.exchange, data.abi_encode())
        }
        PerplWithdraw { amount } => plain(d.perpl.exchange, IPerpl::withdrawCollateralCall { amountCNS: amt(amount)? }.abi_encode()),
        WmonWrap { amount } => {
            let a = amt(amount)?;
            Call { to: token(d, "WMON")?.0, value: a, data: IWMON::depositCall {}.abi_encode(), spend: Some((Address::ZERO, a)), approve: false }
        }
        WmonUnwrap { amount } => plain(token(d, "WMON")?.0, IWMON::withdrawCall { amount: amt(amount)? }.abi_encode()),
        FeeTransfer { amount } => {
            let treasury = d.treasury().ok_or("treasury not set in deployments")?;
            let (usdc, a) = (token(d, "USDC")?.0, amt(amount)?);
            Call { to: usdc, value: U256::ZERO, data: IERC20::transferCall { to: treasury, amount: a }.abi_encode(), spend: Some((usdc, a)), approve: false }
        }
    })
}

fn perp_id(d: &Deployments, perp: &str) -> Result<u64, String> {
    d.perpl.perp_ids.get(perp).copied().ok_or_else(|| format!("unknown perp {perp}"))
}

fn erc20(t: Address) -> Result<Address, String> {
    if t.is_zero() { Err("native MON is not an ERC-20 here; use WMON".into()) } else { Ok(t) }
}

fn lend_pool(d: &Deployments, venue: &str) -> Result<Address, String> {
    match venue {
        "aaveV3" => Ok(d.lending.aave_v3.pool),
        "neverland" => Ok(d.lending.neverland.pool),
        v => Err(format!("unknown lending venue {v}")),
    }
}

fn vault_asset(d: &Deployments, vault: Address) -> Result<String, String> {
    d.lending.erc4626.iter().find(|v| v.vault == vault).map(|v| v.asset.clone()).ok_or_else(|| format!("vault {vault} not allowlisted"))
}

/// stablecoin atoms this call takes from the wallet (all three are 6dp), for the per-step cap.
pub fn stable_spend(d: &Deployments, c: &Call) -> U256 {
    match c.spend {
        Some((t, a)) if STABLES.iter().any(|s| d.tokens.get(*s).is_some_and(|x| x.address == t)) => a,
        _ => U256::ZERO,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use alloy::primitives::hex;

    pub fn dep() -> Deployments {
        let mut d = Deployments::load(concat!(env!("CARGO_MANIFEST_DIR"), "/../../contracts/deployments/143.json")).unwrap();
        d.treasury = "0x7777777777777777777777777777777777777777".into();
        d
    }

    const WALLET: &str = "0x1111111111111111111111111111111111111111";

    fn plan() -> Vec<Step> {
        serde_json::from_str(include_str!("../../../testdata/contracts/executor/user_steps.plan.json")).unwrap()
    }

    // golden: `cast calldata` per plan step, generated by testdata/contracts/executor/golden.sh
    const GOLDEN: &str = include_str!("../../../testdata/contracts/executor/user_steps.golden.txt");

    #[test]
    fn plan_fixture_matches_cast_calldata() {
        let (d, w) = (dep(), WALLET.parse().unwrap());
        let golden: Vec<&str> = GOLDEN.lines().filter(|l| !l.is_empty()).collect();
        let plan = plan();
        assert_eq!(plan.len(), golden.len());
        for (s, g) in plan.iter().zip(golden) {
            let c = build(&d, w, s).unwrap();
            assert_eq!(format!("0x{}", hex::encode(&c.data)), g, "{s:?}");
        }
    }

    #[test]
    fn value_spend_approve() {
        let (d, w) = (dep(), WALLET.parse().unwrap());
        let usdc = d.tokens["USDC"].address;
        let b = |j: serde_json::Value| build(&d, w, &serde_json::from_value(j).unwrap());

        let c = b(serde_json::json!({"action":"kuru.marketBuy","market":"WETH","quote":"100000000","minOut":"0"})).unwrap();
        assert_eq!((c.to, c.value, c.spend, c.approve), (d.kuru.markets["WETH"].orderbook, U256::ZERO, Some((usdc, U256::from(100_000_000))), true));

        // MON sell: native value, floored to sizePrecision (1e10 → 1e8 wei units)
        let c = b(serde_json::json!({"action":"kuru.marketSell","market":"MON","size":"250000000000000000123","minOut":"1"})).unwrap();
        assert_eq!((c.value, c.approve), (U256::from(250_000_000_000_000_000_000u128), false));

        let c = b(serde_json::json!({"action":"wmon.wrap","amount":"5"})).unwrap();
        assert_eq!((c.to, c.value, c.approve), (d.tokens["WMON"].address, U256::from(5), false));
        let c = b(serde_json::json!({"action":"shmonad.deposit","amount":"5"})).unwrap();
        assert_eq!((c.value, c.approve), (U256::from(5), false));
        let c = b(serde_json::json!({"action":"activeVault.deposit","base":"7","quote":"9"})).unwrap();
        assert_eq!((c.value, c.spend, c.approve), (U256::from(7), Some((usdc, U256::from(9))), true));
        let c = b(serde_json::json!({"action":"fee.transfer","amount":"400000"})).unwrap();
        assert_eq!((c.to, c.approve, stable_spend(&d, &c)), (usdc, false, U256::from(400_000)));
        let c = b(serde_json::json!({"action":"kuru.marketSell","market":"WETH","size":"100000000","minOut":"0"})).unwrap();
        assert_eq!(stable_spend(&d, &c), U256::ZERO, "WETH spend is not capped as stable");
    }

    #[test]
    fn rejects_anything_off_the_allowlist() {
        let (d, w) = (dep(), WALLET.parse().unwrap());
        let b = |j: serde_json::Value| serde_json::from_value::<Step>(j).map_err(|e| e.to_string()).and_then(|s| build(&d, w, &s));
        for bad in [
            serde_json::json!({"action":"kuru.marketBuy","market":"PEPE","quote":"1","minOut":"0"}),
            serde_json::json!({"action":"kuru.marketBuy","market":"WETH","quote":"0","minOut":"0"}),
            serde_json::json!({"action":"kuru.marketBuy","market":"WETH","quote":"100000000","minOut":"0","to":"0x2222222222222222222222222222222222222222"}),
            serde_json::json!({"action":"kuru.marketBuy","market":"WETH","quote":"99","minOut":"0"}), // < 1 quote tick
            serde_json::json!({"action":"vault.deposit","vault":"0x2222222222222222222222222222222222222222","amount":"1"}),
            serde_json::json!({"action":"lend.supply","venue":"compound","asset":"USDC","amount":"1"}),
            serde_json::json!({"action":"lend.supply","venue":"aaveV3","asset":"MON","amount":"1"}),
            serde_json::json!({"action":"stable.swap","from":"WETH","to":"AUSD","amount":"1","minOut":"0"}),
            serde_json::json!({"action":"perpl.order","perp":"DOGE","kind":"openShort","price":"1","lot":"1","leverageHdths":200}),
            serde_json::json!({"action":"perpl.order","perp":"BTC","kind":"openLong","price":"1","lot":"1","leverageHdths":200}),
            serde_json::json!({"action":"perpl.order","perp":"BTC","kind":"openShort","price":"1","lot":"1"}),
            serde_json::json!({"action":"perpl.order","perp":"BTC","kind":"openShort","price":"1","lot":"1","leverageHdths":200,"amount":"1"}),
            serde_json::json!({"action":"perpl.order","perp":"BTC","kind":"addMargin"}),
            serde_json::json!({"action":"perpl.order","perp":"BTC","kind":"addMargin","amount":"1","price":"1"}),
            serde_json::json!({"action":"kuru.limit","market":"WETH","side":"buy","price":"4294967296","size":"1","postOnly":true}),
            serde_json::json!({"action":"kuru.cancel","market":"WETH","orderIds":[]}),
            serde_json::json!({"action":"erc20.transfer","to":"0x2222222222222222222222222222222222222222","amount":"1"}),
        ] {
            assert!(b(bad.clone()).is_err(), "{bad}");
        }
        let mut no_treasury = d.clone();
        no_treasury.treasury = String::new();
        assert!(build(&no_treasury, w, &Step::FeeTransfer { amount: "1".into() }).is_err());
    }
}
