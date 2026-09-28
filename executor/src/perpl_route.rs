//! Perpl routes. The account's own wallet signs every call: an account is the
//! msg.sender that opened it, there is no delegated-signer concept on the
//! Exchange. Every write is simulated first, because monad bills the full gas
//! limit of a transaction that reverts.
use alloy_primitives::{keccak256, Address, U256};
use axum::{extract::State, Json};
use serde::Deserialize;
use serde_json::{json, Value};

use crate::{
    gas,
    route::{amount, chain, guarded, signs_for, submit, Done, RouteResponse, Sender, Step},
    rpc::{self, Outcome as TxOutcome, Rpc},
    venues::{account::approve_if_needed, perpl, Call},
    AppState,
};

#[derive(Debug, Clone, Deserialize)]
pub struct AccountRequest {
    pub privy_wallet_id: String,
    pub user_wallet: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct FundsRequest {
    pub intent_key: String,
    pub privy_wallet_id: String,
    pub user_wallet: String,
    /// collateral atoms (AUSD has 6 decimals)
    pub amount: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct OrderRequest {
    pub intent_key: String,
    pub privy_wallet_id: String,
    pub user_wallet: String,
    /// perpl's numeric perp id, as the context names it
    pub market_id: String,
    /// buy opens or adds long, sell opens or adds short. ignored by close.
    #[serde(default)]
    pub side: String,
    /// market | limit | close
    pub kind: String,
    /// LNS: size * 10^size_decimals. close: empty or 0 closes the whole position.
    #[serde(default)]
    pub lot: String,
    /// PNS, limit only
    #[serde(default)]
    pub price: String,
    /// hundredths, 0 = default
    #[serde(default)]
    pub leverage: u64,
    #[serde(default)]
    pub post_only: bool,
    #[serde(default)]
    pub max_slippage_bps: u32,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CancelRequest {
    pub intent_key: String,
    pub privy_wallet_id: String,
    pub user_wallet: String,
    pub market_id: String,
    pub order_id: String,
}

fn user(s: &str) -> Result<Address, String> {
    s.trim().parse().map_err(|_| format!("user_wallet {s:?} is not an address"))
}

/// the intent key, carried on chain as orderDescId so the log names its intent
fn client_id(intent_key: &str) -> u64 {
    u64::from_be_bytes(keccak256(intent_key)[..8].try_into().unwrap())
}

async fn opened(rpc: &Rpc, v: &perpl::Venue, who: Address) -> Result<perpl::AccountInfo, String> {
    perpl::account(rpc, v.exchange, who)
        .await?
        .ok_or_else(|| format!("{who} has no perpl account: deposit at least {} {} first", human(v.instance.min_account_open_amount, v.collateral.decimals), v.collateral.symbol))
}

fn human(v: U256, decimals: u32) -> String {
    let s = v.to_string();
    let d = decimals as usize;
    if d == 0 {
        return s;
    }
    let s = format!("{s:0>width$}", width = d + 1);
    let (w, f) = s.split_at(s.len() - d);
    let f = f.trim_end_matches('0');
    if f.is_empty() { w.to_string() } else { format!("{w}.{f}") }
}

/// refuses a call the chain would revert, naming the error, before gas is spent
async fn simulate(rpc: &Rpc, from: Address, call: &Call) -> Result<(), String> {
    rpc.eth_call(from, call.to, call.value, &call.data).await.map(|_| ()).map_err(|e| perpl::revert_reason(&e))
}

async fn landed(st: &AppState, rpc: &Rpc, hash: &str, step: &str) -> Result<Option<Vec<perpl::Log>>, String> {
    match rpc::wait(|| rpc.receipt_logs(hash), st.receipt_timeout, rpc::POLL_INTERVAL).await {
        Some((TxOutcome::Confirmed, logs)) => Ok(Some(logs)),
        Some((TxOutcome::Reverted, _)) => Err(format!("{step} reverted: {hash}")),
        None => Ok(None),
    }
}

// ---------------------------------------------------------------- account

pub async fn account(State(st): State<AppState>, Json(req): Json<AccountRequest>) -> Json<Value> {
    Json(read_account(&st, &req).await.unwrap_or_else(|e| json!({"error": e})))
}

async fn read_account(st: &AppState, req: &AccountRequest) -> Result<Value, String> {
    let v = st.perpl.venue().await?;
    let (_, rpc) = chain(st, v.chain_id)?;
    let who = user(&req.user_wallet)?;
    signs_for(st, &req.privy_wallet_id, who).await?;

    let base = json!({
        "chain_id": v.chain_id,
        "exchange": v.exchange,
        "collateral": {"symbol": v.collateral.symbol, "address": v.collateral.address, "decimals": v.collateral.decimals},
        "min_open": v.instance.min_account_open_amount.to_string(),
        "min_deposit": v.instance.min_deposit_amount.to_string(),
        "min_withdraw": v.instance.min_withdraw_amount.to_string(),
    });
    let mut out = base.as_object().cloned().unwrap_or_default();

    let Some(a) = perpl::account(rpc, v.exchange, who).await? else {
        out.insert("registered".into(), json!(false));
        out.insert("positions".into(), json!([]));
        return Ok(Value::Object(out));
    };
    let mut positions = Vec::new();
    for perp_id in perpl::position_perps(&a.positions) {
        let Some(p) = v.perps.iter().find(|p| p.id == perp_id) else { continue };
        let pos = perpl::position(rpc, v.exchange, perp_id, a.accountId).await?;
        let info = pos.positionInfo;
        if info.lotLNS.is_zero() {
            continue;
        }
        let long = info.positionType == perpl::LONG;
        let liq = perpl::liquidation_price(long, info.pricePNS, info.lotLNS, info.depositCNS, p, v.collateral.decimals);
        positions.push(json!({
            "market_id": perp_id.to_string(),
            "symbol": p.symbol,
            "side": if long { "long" } else { "short" },
            "lot": info.lotLNS.to_string(),
            "entry_price": info.pricePNS.to_string(),
            "deposit": info.depositCNS.to_string(),
            "pnl": info.pnlCNS.to_string(),
            "mark_price": pos.markPriceValid.then(|| pos.markPricePNS.to_string()),
            "liquidation_price_est": liq.map(|l| l.to_string()),
            "price_decimals": p.config.price_decimals,
            "size_decimals": p.config.size_decimals,
        }));
    }
    out.insert("registered".into(), json!(true));
    out.insert("account_id".into(), json!(a.accountId.to_string()));
    out.insert("balance".into(), json!(a.balanceCNS.to_string()));
    out.insert("locked".into(), json!(a.lockedBalanceCNS.to_string()));
    out.insert("frozen".into(), json!(a.frozen != 0));
    out.insert("positions".into(), json!(positions));
    Ok(Value::Object(out))
}

// ---------------------------------------------------------------- funds

pub async fn deposit(State(st): State<AppState>, Json(req): Json<FundsRequest>) -> Json<RouteResponse> {
    let (key, wallet) = (req.intent_key.clone(), req.privy_wallet_id.clone());
    let inner = st.clone();
    guarded(&st, &key, &wallet, move |mut steps| async move {
        let out = do_deposit(&inner, &req, &mut steps).await;
        (out, steps)
    })
    .await
}

async fn do_deposit(st: &AppState, req: &FundsRequest, steps: &mut Vec<Step>) -> Result<Done, String> {
    let v = st.perpl.venue().await?;
    let (chain_id, rpc) = chain(st, v.chain_id)?;
    let who = user(&req.user_wallet)?;
    let amt = amount(&req.amount, "amount")?;
    signs_for(st, &req.privy_wallet_id, who).await?;
    let sender = Sender { wallet_id: req.privy_wallet_id.clone(), from: who };

    let existing = perpl::account(rpc, v.exchange, who).await?;
    let (min, call, step) = match &existing {
        None => (v.instance.min_account_open_amount, perpl::create_account_call(v.exchange, amt), "create_account"),
        Some(_) => (v.instance.min_deposit_amount, perpl::deposit_call(v.exchange, amt), "deposit"),
    };
    if amt < min {
        return Err(format!("{step} needs at least {} {}", human(min, v.collateral.decimals), v.collateral.symbol));
    }

    let token = v.collateral.address;
    if let Some(approve) = approve_if_needed(rpc, token, v.exchange, who, amt).await {
        let hash = submit(st, chain_id, &req.intent_key, &sender, "approve", &approve, gas::APPROVE).await?;
        steps.push(Step::new("approve", "submitted", &hash, token.to_string()));
        if landed(st, rpc, &hash, "approve").await?.is_none() {
            return Err(format!("approve not mined within {:?}: {hash}", st.receipt_timeout));
        }
        steps.last_mut().unwrap().status = "confirmed".into();
    } else {
        // the approval already covers it, so the call can be proven before paying for it
        simulate(rpc, who, &call).await?;
    }

    let hash = submit(st, chain_id, &req.intent_key, &sender, step, &call, gas::PERPL_ACCOUNT).await?;
    steps.push(Step::new(step, "submitted", &hash, format!("{} {}", human(amt, v.collateral.decimals), v.collateral.symbol)));
    if landed(st, rpc, &hash, step).await?.is_none() {
        return Ok(Done::new(&hash, "submitted", json!({"reason": "receipt_timeout"})));
    }
    steps.last_mut().unwrap().status = "confirmed".into();
    let a = opened(rpc, &v, who).await?;
    Ok(Done::new(&hash, "confirmed", json!({"account_id": a.accountId.to_string(), "balance": a.balanceCNS.to_string()})))
}

pub async fn withdraw(State(st): State<AppState>, Json(req): Json<FundsRequest>) -> Json<RouteResponse> {
    let (key, wallet) = (req.intent_key.clone(), req.privy_wallet_id.clone());
    let inner = st.clone();
    guarded(&st, &key, &wallet, move |mut steps| async move {
        let out = do_withdraw(&inner, &req, &mut steps).await;
        (out, steps)
    })
    .await
}

async fn do_withdraw(st: &AppState, req: &FundsRequest, steps: &mut Vec<Step>) -> Result<Done, String> {
    let v = st.perpl.venue().await?;
    let (chain_id, rpc) = chain(st, v.chain_id)?;
    let who = user(&req.user_wallet)?;
    let amt = amount(&req.amount, "amount")?;
    signs_for(st, &req.privy_wallet_id, who).await?;
    let a = opened(rpc, &v, who).await?;
    let free = a.balanceCNS.saturating_sub(a.lockedBalanceCNS);
    if amt < v.instance.min_withdraw_amount {
        return Err(format!("withdrawals start at {} {}", human(v.instance.min_withdraw_amount, v.collateral.decimals), v.collateral.symbol));
    }
    if amt > free {
        return Err(format!("only {} {} is free to withdraw; the rest backs open orders or positions", human(free, v.collateral.decimals), v.collateral.symbol));
    }
    let sender = Sender { wallet_id: req.privy_wallet_id.clone(), from: who };
    let call = perpl::withdraw_call(v.exchange, amt);
    simulate(rpc, who, &call).await?;
    let hash = submit(st, chain_id, &req.intent_key, &sender, "withdraw", &call, gas::PERPL_ACCOUNT).await?;
    steps.push(Step::new("withdraw", "submitted", &hash, human(amt, v.collateral.decimals)));
    if landed(st, rpc, &hash, "withdraw").await?.is_none() {
        return Ok(Done::new(&hash, "submitted", json!({"reason": "receipt_timeout"})));
    }
    steps.last_mut().unwrap().status = "confirmed".into();
    let a = opened(rpc, &v, who).await?;
    Ok(Done::new(&hash, "confirmed", json!({"balance": a.balanceCNS.to_string()})))
}

// ---------------------------------------------------------------- orders

pub async fn order(State(st): State<AppState>, Json(req): Json<OrderRequest>) -> Json<RouteResponse> {
    let (key, wallet) = (req.intent_key.clone(), req.privy_wallet_id.clone());
    let inner = st.clone();
    guarded(&st, &key, &wallet, move |mut steps| async move {
        let out = place(&inner, &req, &mut steps).await;
        (out, steps)
    })
    .await
}

fn lot(s: &str) -> Result<U256, String> {
    if s.trim().is_empty() {
        return Ok(U256::ZERO);
    }
    U256::from_str_radix(s.trim(), 10).map_err(|_| format!("lot {s:?} is not an integer"))
}

async fn place(st: &AppState, req: &OrderRequest, steps: &mut Vec<Step>) -> Result<Done, String> {
    let v = st.perpl.venue().await?;
    let (chain_id, rpc) = chain(st, v.chain_id)?;
    let p = v.perp(&req.market_id)?.clone();
    let who = user(&req.user_wallet)?;
    signs_for(st, &req.privy_wallet_id, who).await?;
    let a = opened(rpc, &v, who).await?;
    let head = rpc.block_number().await.ok_or("monad rpc did not answer the block number")?;

    let mut spec = perpl::OrderSpec {
        client_id: client_id(&req.intent_key),
        perp_id: p.id,
        order_type: perpl::OPEN_LONG,
        order_id: 0,
        price: U256::ZERO,
        lot: lot(&req.lot)?,
        post_only: false,
        ioc: false,
        leverage: 0,
        // a stale order is refused on chain rather than filled late
        last_block: head + p.order_ttl_blocks.max(1),
        max_neg_pnl_bps: p.order_max_neg_pnl_collat_bps,
    };
    match req.kind.trim().to_ascii_lowercase().as_str() {
        "close" => {
            let pos = perpl::position(rpc, v.exchange, p.id, a.accountId).await?.positionInfo;
            if pos.lotLNS.is_zero() {
                return Err(format!("no open {} position to close", p.symbol));
            }
            let long = pos.positionType == perpl::LONG;
            if spec.lot.is_zero() {
                spec.lot = pos.lotLNS;
            }
            if spec.lot > pos.lotLNS {
                return Err(format!("close lot {} exceeds the position's {}", spec.lot, pos.lotLNS));
            }
            spec.order_type = if long { perpl::CLOSE_LONG } else { perpl::CLOSE_SHORT };
            // closing a long sells, closing a short buys
            spec.price = perpl::worst_price(!long, &p, req.max_slippage_bps)?;
            spec.ioc = true;
        }
        kind @ ("market" | "limit") => {
            let long = match req.side.trim().to_ascii_lowercase().as_str() {
                "buy" | "long" => true,
                "sell" | "short" => false,
                s => return Err(format!("side must be buy or sell, not {s:?}")),
            };
            if spec.lot.is_zero() {
                return Err("lot must be a positive integer of size units".into());
            }
            spec.order_type = if long { perpl::OPEN_LONG } else { perpl::OPEN_SHORT };
            spec.leverage = perpl::leverage(req.leverage, &p)?;
            if kind == "market" {
                spec.price = perpl::worst_price(long, &p, req.max_slippage_bps)?;
                spec.ioc = true;
            } else {
                spec.price = amount(&req.price, "price")?;
                spec.post_only = req.post_only;
            }
        }
        k => return Err(format!("kind must be market, limit or close, not {k:?}")),
    }

    let call = perpl::order_call(v.exchange, &spec);
    simulate(rpc, who, &call).await?;
    let sender = Sender { wallet_id: req.privy_wallet_id.clone(), from: who };
    let fallback = if spec.ioc { gas::PERPL_TAKE } else { gas::PERPL_POST };
    let hash = submit(st, chain_id, &req.intent_key, &sender, "order", &call, fallback).await?;
    steps.push(Step::new("order", "submitted", &hash, format!("{} {} lot {} @ {}", p.symbol, req.kind, spec.lot, spec.price)));

    let base = json!({
        "perp_id": p.id,
        "lot": spec.lot.to_string(),
        "bound_price": spec.price.to_string(),
        "leverage": spec.leverage,
        "price_decimals": p.config.price_decimals,
        "size_decimals": p.config.size_decimals,
    });
    let Some(logs) = landed(st, rpc, &hash, "order").await? else {
        return Ok(Done::new(&hash, "submitted", merge(base, json!({"reason": "receipt_timeout"}))));
    };
    steps.last_mut().unwrap().status = "confirmed".into();
    let o = perpl::Outcome::from_logs(v.exchange, &logs);
    Ok(Done::new(
        &hash,
        o.status(spec.lot),
        merge(
            base,
            json!({
                "filled_lot": o.filled_lot.to_string(),
                "avg_price": o.avg_price().map(|p| p.to_string()),
                "fee": o.fee.to_string(),
                "order_id": o.rested_order_id.map(|id| id.to_string()),
                "rested_lot": o.rested_lot.to_string(),
            }),
        ),
    ))
}

fn merge(mut a: Value, b: Value) -> Value {
    if let (Some(x), Some(y)) = (a.as_object_mut(), b.as_object()) {
        x.extend(y.clone());
    }
    a
}

pub async fn cancel(State(st): State<AppState>, Json(req): Json<CancelRequest>) -> Json<RouteResponse> {
    let (key, wallet) = (req.intent_key.clone(), req.privy_wallet_id.clone());
    let inner = st.clone();
    guarded(&st, &key, &wallet, move |mut steps| async move {
        let out = do_cancel(&inner, &req, &mut steps).await;
        (out, steps)
    })
    .await
}

async fn do_cancel(st: &AppState, req: &CancelRequest, steps: &mut Vec<Step>) -> Result<Done, String> {
    let v = st.perpl.venue().await?;
    let (chain_id, rpc) = chain(st, v.chain_id)?;
    // a closed market still lets resting orders be cancelled
    let perp_id: u64 = req.market_id.trim().parse().map_err(|_| format!("market_id {:?} is not a number", req.market_id))?;
    let order_id: u64 = req.order_id.trim().parse().map_err(|_| format!("order_id {:?} is not a number", req.order_id))?;
    let who = user(&req.user_wallet)?;
    signs_for(st, &req.privy_wallet_id, who).await?;
    let a = opened(rpc, &v, who).await?;

    // order ids are u16 and recycled: cancel only what is provably ours right now
    let live = perpl::order(rpc, v.exchange, perp_id, order_id).await?;
    if U256::from(live.accountId) != a.accountId {
        return Err(format!("order {order_id} on perp {perp_id} is no longer this account's (filled, cancelled or reused)"));
    }
    let spec = perpl::OrderSpec {
        client_id: client_id(&req.intent_key),
        perp_id,
        order_type: perpl::CANCEL,
        order_id,
        price: U256::ZERO,
        lot: U256::ZERO,
        post_only: false,
        ioc: false,
        leverage: 0,
        last_block: 0,
        max_neg_pnl_bps: 0,
    };
    let call = perpl::order_call(v.exchange, &spec);
    simulate(rpc, who, &call).await?;
    let sender = Sender { wallet_id: req.privy_wallet_id.clone(), from: who };
    let hash = submit(st, chain_id, &req.intent_key, &sender, "cancel", &call, gas::PERPL_POST).await?;
    steps.push(Step::new("cancel", "submitted", &hash, format!("perp {perp_id} order {order_id}")));
    let Some(logs) = landed(st, rpc, &hash, "cancel").await? else {
        return Ok(Done::new(&hash, "submitted", json!({"reason": "receipt_timeout"})));
    };
    steps.last_mut().unwrap().status = "confirmed".into();
    let o = perpl::Outcome::from_logs(v.exchange, &logs);
    Ok(Done::new(&hash, if o.canceled { "canceled" } else { "not_canceled" }, json!({"perp_id": perp_id, "order_id": order_id.to_string()})))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn human_amounts() {
        assert_eq!(human(U256::from(100_000_000u64), 6), "100");
        assert_eq!(human(U256::from(10_000u64), 6), "0.01");
        assert_eq!(human(U256::from(1_500_000u64), 6), "1.5");
        assert_eq!(human(U256::from(7u64), 0), "7");
    }

    #[test]
    fn client_id_is_stable_per_intent() {
        assert_eq!(client_id("a"), client_id("a"));
        assert_ne!(client_id("a"), client_id("b"));
    }

    #[test]
    fn order_request_defaults() {
        let r: OrderRequest = serde_json::from_value(json!({
            "intent_key": "k", "privy_wallet_id": "w", "user_wallet": "0x1111111111111111111111111111111111111111",
            "market_id": "16", "kind": "close"
        }))
        .unwrap();
        assert_eq!((r.lot.as_str(), r.leverage, r.post_only), ("", 0, false));
        assert_eq!(lot(&r.lot).unwrap(), U256::ZERO, "an empty close lot means the whole position");
    }
}
