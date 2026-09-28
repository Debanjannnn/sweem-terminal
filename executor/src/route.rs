use alloy_primitives::{keccak256, Address, U256};
use axum::{extract::State, Json};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use tracing::{info, warn};

use crate::{
    gas, idem,
    rpc::{self, Outcome, Rpc},
    venues::{account, clamp_slippage, kuru, Call, Market, Side, Venue},
    AppState,
};

const SWAP_DEADLINE_SECS: u64 = 60;
const DEFAULT_GRANT_SECS: u64 = 30 * 24 * 3600;

#[derive(Debug, Clone, Deserialize)]
pub struct RouteRequest {
    pub intent_key: String,
    pub privy_wallet_id: String,
    pub user_wallet: String,
    /// a name, never an address: the target is resolved from the allowlist
    pub market_id: String,
    pub venue: String,
    pub side: String,
    /// raw atoms of the input token: quote for a buy, base for a sell
    pub amount: String,
    #[serde(default)]
    pub max_slippage_bps: u32,
    #[serde(default)]
    pub chain_id: u64,
}

#[derive(Debug, Clone, Deserialize)]
pub struct AccountRequest {
    pub privy_wallet_id: String,
    pub user_wallet: String,
    #[serde(default)]
    pub chain_id: u64,
}

#[derive(Debug, Clone, Deserialize)]
pub struct DepositRequest {
    pub intent_key: String,
    pub privy_wallet_id: String,
    pub user_wallet: String,
    pub market_id: String,
    /// "base" or "quote": a name, never an address
    pub token: String,
    pub amount: String,
    #[serde(default)]
    pub chain_id: u64,
}

#[derive(Debug, Clone, Deserialize)]
pub struct SignerRequest {
    pub intent_key: String,
    pub privy_wallet_id: String,
    pub user_wallet: String,
    /// unix seconds; 0 defaults to 30 days. never 0-means-forever.
    #[serde(default)]
    pub expiry: u64,
    #[serde(default)]
    pub chain_id: u64,
}

#[derive(Debug, Clone, Deserialize)]
pub struct OrderRequest {
    pub intent_key: String,
    pub privy_wallet_id: String,
    pub user_wallet: String,
    pub market_id: String,
    pub side: String,
    /// raw tick integer, price * price_precision
    pub price: String,
    /// book units, size * size_precision
    pub quantity: String,
    #[serde(default)]
    pub tif: String,
    #[serde(default)]
    pub post_only: bool,
    #[serde(default)]
    pub chain_id: u64,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CancelRequest {
    pub intent_key: String,
    pub privy_wallet_id: String,
    pub user_wallet: String,
    pub market_id: String,
    pub slot_idx: u8,
    /// the order the caller believes occupies that slot. slots are reused.
    pub expected_order_id: String,
    #[serde(default)]
    pub chain_id: u64,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct RouteResponse {
    pub intent_key: String,
    pub tx_hash: String,
    pub status: String,
    pub steps: Vec<Step>,
    pub error: String,
    #[serde(default, skip_serializing_if = "Value::is_null")]
    pub info: Value,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct Step {
    pub name: String,
    pub status: String,
    #[serde(default)]
    pub tx_hash: String,
    #[serde(default)]
    pub detail: String,
}

impl Step {
    pub(crate) fn new(name: &str, status: &str, tx_hash: &str, detail: String) -> Self {
        Self { name: name.into(), status: status.into(), tx_hash: tx_hash.into(), detail }
    }
}

pub(crate) struct Done {
    tx_hash: String,
    status: String,
    info: Value,
}

impl Done {
    pub(crate) fn new(tx_hash: &str, status: &str, info: Value) -> Self {
        Self { tx_hash: tx_hash.into(), status: status.into(), info }
    }
}

/// who signs and pays. the backend signer holds TRADE on the account; without one
/// the user's own wallet signs for its own account.
pub(crate) struct Sender {
    pub(crate) wallet_id: String,
    pub(crate) from: Address,
}

fn now() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0)
}

pub(crate) fn amount(s: &str, what: &str) -> Result<U256, String> {
    let v = U256::from_str_radix(s.trim(), 10).map_err(|_| format!("{what} {s:?} is not a uint256"))?;
    if v.is_zero() {
        return Err(format!("{what} must be greater than zero"));
    }
    Ok(v)
}

/// the intent key, carried on chain so the resulting logs can be tied back to it.
fn client_order_id(intent_key: &str) -> [u8; 32] {
    keccak256(intent_key).0
}

pub(crate) async fn signs_for(st: &AppState, wallet_id: &str, expect: Address) -> Result<(), String> {
    let w = st.privy.get_wallet(wallet_id).await.map_err(|e| format!("privy wallet lookup failed: {e}"))?;
    let owner: Address = w.address.parse().map_err(|_| format!("privy returned a bad address for {wallet_id}"))?;
    if owner != expect {
        return Err(format!("privy wallet {wallet_id} is {owner}, not {expect}"));
    }
    Ok(())
}

pub(crate) fn chain(st: &AppState, chain_id: u64) -> Result<(u64, &Rpc), String> {
    let id = if chain_id == 0 { crate::config::DEFAULT_CHAIN_ID } else { chain_id };
    st.rpc.get(&id).map(|r| (id, r)).ok_or(format!("no rpc for chain {id}"))
}

fn market(st: &AppState, id: &str, chain_id: u64) -> Result<Market, String> {
    // the only source of a callable market address
    let m = st.markets.get(id).ok_or_else(|| format!("market {id:?} is not on the allowlist"))?.clone();
    if chain_id != 0 && chain_id != m.chain_id {
        return Err(format!("market {} is on chain {}, not {}", m.id, m.chain_id, chain_id));
    }
    Ok(m)
}

/// every trading call names the account by id, so an unregistered address is a
/// hard stop: a wallet balance cannot trade.
async fn trading_account(st: &AppState, rpc: &Rpc, user: Address) -> Result<u64, String> {
    match account::user_id(rpc, st.account_core, user).await? {
        0 => Err(format!("{user} has no kuru account: deposit into AccountCore first")),
        id => Ok(id),
    }
}

/// the wallet the nonce lease and the transaction belong to.
fn sender_wallet(st: &AppState, privy_wallet_id: &str) -> String {
    st.signer.as_ref().map(|s| s.wallet_id.clone()).unwrap_or_else(|| privy_wallet_id.into())
}

fn trading_sender(st: &AppState, privy_wallet_id: &str, user: Address) -> Sender {
    match &st.signer {
        Some(s) => Sender { wallet_id: s.wallet_id.clone(), from: s.address },
        None => Sender { wallet_id: privy_wallet_id.into(), from: user },
    }
}

pub(crate) async fn submit(st: &AppState, chain_id: u64, intent_key: &str, sender: &Sender, step: &str, call: &Call, fallback_gas: u64) -> Result<String, String> {
    let (_, rpc) = chain(st, chain_id)?;
    let limit = gas::limit(rpc, sender.from, call.to, call.value, &call.data, fallback_gas).await;
    let fees = gas::fees(rpc, limit).await;
    gas::affordable(rpc, sender.from, &fees, call.value).await?;
    let tx = st
        .privy
        .send_transaction(
            &sender.wallet_id,
            chain_id,
            call,
            &fees,
            &idem::privy_key(intent_key, step),
            &idem::reference_id(intent_key, step),
        )
        .await
        .map_err(|e| format!("{step} failed: {e}"))?;
    info!(step, hash = %tx.hash, gas_limit = fees.gas_limit, "submitted");
    Ok(tx.hash)
}

pub(crate) async fn mined(st: &AppState, rpc: &Rpc, hash: &str, step: &str) -> Result<bool, String> {
    match rpc::wait(|| rpc.receipt(hash), st.receipt_timeout, rpc::POLL_INTERVAL).await {
        Some(Outcome::Confirmed) => Ok(true),
        Some(Outcome::Reverted) => Err(format!("{step} reverted: {hash}")),
        None => Ok(false),
    }
}

/// one recorded answer per intent_key, serialized on the wallet that signs.
pub(crate) async fn guarded<F, Fut>(st: &AppState, intent_key: &str, wallet_id: &str, run: F) -> Json<RouteResponse>
where
    F: FnOnce(Vec<Step>) -> Fut,
    Fut: std::future::Future<Output = (Result<Done, String>, Vec<Step>)>,
{
    if let Some(done) = st.idem.get(intent_key) {
        info!(intent_key, "replaying recorded result");
        return Json(done);
    }
    // serialized per wallet: privy assigns the nonce
    let _lease = st.nonces.lease(wallet_id).await;
    if let Some(done) = st.idem.get(intent_key) {
        return Json(done);
    }

    let (out, steps) = run(Vec::new()).await;
    let resp = match out {
        Ok(d) => RouteResponse {
            intent_key: intent_key.into(),
            tx_hash: d.tx_hash,
            status: d.status,
            steps,
            error: String::new(),
            info: d.info,
        },
        Err(e) => {
            warn!(intent_key, error = %e, "execution failed");
            RouteResponse { intent_key: intent_key.into(), status: "failed".into(), steps, error: e, ..Default::default() }
        }
    };
    st.idem.put(intent_key, &resp);
    Json(resp)
}

/// GET-shaped read: what `wallet` needs before it can trade for a user.
pub async fn account_ensure(State(st): State<AppState>, Json(req): Json<AccountRequest>) -> Json<Value> {
    Json(match ensure(&st, &req).await {
        Ok(v) => v,
        Err(e) => json!({"error": e}),
    })
}

async fn ensure(st: &AppState, req: &AccountRequest) -> Result<Value, String> {
    let (chain_id, rpc) = chain(st, req.chain_id)?;
    let user: Address = req.user_wallet.trim().parse().map_err(|_| format!("user_wallet {:?} is not an address", req.user_wallet))?;
    signs_for(st, &req.privy_wallet_id, user).await?;

    let id = account::user_id(rpc, st.account_core, user).await?;
    let epoch = account::auth_nonce(rpc, st.account_core, user).await?;
    let signer = st.signer.as_ref().map(|s| s.address);
    let authorized = match signer {
        Some(s) if id != 0 => account::is_authorized(rpc, st.account_core, user, s).await?,
        Some(_) => false,
        // no backend signer: trades are signed by the owner's own wallet, which
        // is the account and needs no TRADE grant. reporting false here left
        // every order refused and /account/authorize with nobody to authorize.
        None => true,
    };
    Ok(json!({
        "chain_id": chain_id,
        "account_core": st.account_core,
        "user_wallet": user,
        "user_id": id,
        "registered": id != 0,
        "signer": signer,
        "signer_authorized": authorized,
        // every grant or revoke advances this and invalidates pre-signed intents
        "auth_nonce": epoch.to_string(),
    }))
}

/// deposit into AccountCore. funds must sit here before anything can trade.
pub async fn account_deposit(State(st): State<AppState>, Json(req): Json<DepositRequest>) -> Json<RouteResponse> {
    let (key, wallet) = (req.intent_key.clone(), req.privy_wallet_id.clone());
    let inner = st.clone();
    guarded(&st, &key, &wallet, move |mut steps| async move {
        let out = deposit(&inner, &req, &mut steps).await;
        (out, steps)
    })
    .await
}

async fn deposit(st: &AppState, req: &DepositRequest, steps: &mut Vec<Step>) -> Result<Done, String> {
    let m = market(st, &req.market_id, req.chain_id)?;
    let (chain_id, rpc) = chain(st, m.chain_id)?;
    let token = m.token(&req.token).ok_or_else(|| format!("token {:?} must be \"base\" or \"quote\"", req.token))?;
    let amt = amount(&req.amount, "amount")?;
    let user: Address = req.user_wallet.trim().parse().map_err(|_| format!("user_wallet {:?} is not an address", req.user_wallet))?;
    // the funds leave the user's wallet, so the user's wallet signs
    signs_for(st, &req.privy_wallet_id, user).await?;
    let sender = Sender { wallet_id: req.privy_wallet_id.clone(), from: user };

    if let Some(call) = account::approve_if_needed(rpc, token, st.account_core, user, amt).await {
        let hash = submit(st, chain_id, &req.intent_key, &sender, "approve", &call, gas::APPROVE).await?;
        steps.push(Step::new("approve", "submitted", &hash, token.to_string()));
        // the deposit pulls against this allowance, so it has to land first
        if !mined(st, rpc, &hash, "approve").await? {
            return Err(format!("approve not mined within {:?}: {hash}", st.receipt_timeout));
        }
        steps.last_mut().unwrap().status = "confirmed".into();
    }

    let call = account::deposit_call(st.account_core, token, amt);
    let hash = submit(st, chain_id, &req.intent_key, &sender, "deposit", &call, gas::DEPOSIT).await?;
    steps.push(Step::new("deposit", "submitted", &hash, format!("{token} {amt}")));
    if !mined(st, rpc, &hash, "deposit").await? {
        return Ok(Done::new(&hash, "submitted", json!({"reason": "receipt_timeout"})));
    }
    steps.last_mut().unwrap().status = "confirmed".into();

    // the first deposit is what creates the account id
    let id = account::user_id(rpc, st.account_core, user).await?;
    let (free, reserved) = account::balances(rpc, st.account_core, user, token).await?;
    Ok(Done::new(
        &hash,
        "confirmed",
        json!({"user_id": id, "registered": id != 0, "token": token, "free": free.to_string(), "reserved": reserved.to_string()}),
    ))
}

/// grant TRADE to the backend signer. the account owner signs this one.
pub async fn account_authorize(State(st): State<AppState>, Json(req): Json<SignerRequest>) -> Json<RouteResponse> {
    let (key, wallet) = (req.intent_key.clone(), req.privy_wallet_id.clone());
    let inner = st.clone();
    guarded(&st, &key, &wallet, move |mut steps| async move {
        let out = grant(&inner, &req, &mut steps, true).await;
        (out, steps)
    })
    .await
}

pub async fn account_revoke(State(st): State<AppState>, Json(req): Json<SignerRequest>) -> Json<RouteResponse> {
    let (key, wallet) = (req.intent_key.clone(), req.privy_wallet_id.clone());
    let inner = st.clone();
    guarded(&st, &key, &wallet, move |mut steps| async move {
        let out = grant(&inner, &req, &mut steps, false).await;
        (out, steps)
    })
    .await
}

async fn grant(st: &AppState, req: &SignerRequest, steps: &mut Vec<Step>, authorize: bool) -> Result<Done, String> {
    let (chain_id, rpc) = chain(st, req.chain_id)?;
    let signer = st.signer.as_ref().ok_or("no backend signer is configured")?.address;
    let user: Address = req.user_wallet.trim().parse().map_err(|_| format!("user_wallet {:?} is not an address", req.user_wallet))?;
    signs_for(st, &req.privy_wallet_id, user).await?;
    let sender = Sender { wallet_id: req.privy_wallet_id.clone(), from: user };

    let (step, call) = if authorize {
        let expiry = if req.expiry == 0 { now() + DEFAULT_GRANT_SECS } else { req.expiry };
        if expiry <= now() {
            return Err(format!("expiry {expiry} is already past"));
        }
        ("authorize", account::authorize_call(st.account_core, user, signer, expiry))
    } else {
        ("revoke", account::revoke_call(st.account_core, user, signer))
    };

    let hash = submit(st, chain_id, &req.intent_key, &sender, step, &call, gas::AUTHORIZE).await?;
    steps.push(Step::new(step, "submitted", &hash, signer.to_string()));
    if !mined(st, rpc, &hash, step).await? {
        return Ok(Done::new(&hash, "submitted", json!({"reason": "receipt_timeout"})));
    }
    steps.last_mut().unwrap().status = "confirmed".into();

    // expiry emits no event, so read the live answer back rather than infer it
    let live = account::is_authorized(rpc, st.account_core, user, signer).await?;
    let epoch = account::auth_nonce(rpc, st.account_core, user).await?;
    Ok(Done::new(&hash, "confirmed", json!({"signer": signer, "signer_authorized": live, "auth_nonce": epoch.to_string()})))
}

/// taker leg: OrderBook.swap, exact input, raw token atoms.
pub async fn route(State(st): State<AppState>, Json(req): Json<RouteRequest>) -> Json<RouteResponse> {
    let (key, wallet) = (req.intent_key.clone(), sender_wallet(&st, &req.privy_wallet_id));
    let inner = st.clone();
    guarded(&st, &key, &wallet, move |mut steps| async move {
        let out = swap(&inner, &req, &mut steps).await;
        (out, steps)
    })
    .await
}

async fn swap(st: &AppState, req: &RouteRequest, steps: &mut Vec<Step>) -> Result<Done, String> {
    if Venue::parse(&req.venue).is_none() {
        return Err(format!("unknown venue {:?}", req.venue));
    }
    let side = Side::parse(&req.side).ok_or_else(|| format!("unknown side {:?}", req.side))?;
    let m = market(st, &req.market_id, req.chain_id)?;
    let (chain_id, rpc) = chain(st, m.chain_id)?;
    let amt = amount(&req.amount, "amount")?;
    let user: Address = req.user_wallet.trim().parse().map_err(|_| format!("user_wallet {:?} is not an address", req.user_wallet))?;
    signs_for(st, &req.privy_wallet_id, user).await?;
    let id = trading_account(st, rpc, user).await?;
    let sender = trading_sender(st, &req.privy_wallet_id, user);
    let slippage = clamp_slippage(req.max_slippage_bps);

    // refuse to swap without a slippage bound rather than submit an unprotected one
    let quoted = kuru::estimate_swap(rpc, sender.from, &m, id, side, amt).await.map_err(|e| format!("quote simulation failed: {e}"))?;
    let min_out = kuru::min_out(quoted, slippage);
    steps.push(Step::new("quote", "ok", "", format!("out={quoted} min_out={min_out} bps={slippage}")));

    let call = kuru::swap_call(&m, id, side, amt, min_out, now() + SWAP_DEADLINE_SECS)?;
    let hash = submit(st, chain_id, &req.intent_key, &sender, "swap", &call, gas::SWAP).await?;
    steps.push(Step::new("swap", "submitted", &hash, m.id.clone()));
    Ok(Done::new(&hash, "submitted", json!({"user_id": id, "quoted": quoted.to_string(), "min_out": min_out.to_string()})))
}

/// maker leg: OrderBook.batch with one order.
pub async fn order(State(st): State<AppState>, Json(req): Json<OrderRequest>) -> Json<RouteResponse> {
    let (key, wallet) = (req.intent_key.clone(), sender_wallet(&st, &req.privy_wallet_id));
    let inner = st.clone();
    guarded(&st, &key, &wallet, move |mut steps| async move {
        let out = place(&inner, &req, &mut steps).await;
        (out, steps)
    })
    .await
}

async fn place(st: &AppState, req: &OrderRequest, steps: &mut Vec<Step>) -> Result<Done, String> {
    let side = Side::parse(&req.side).ok_or_else(|| format!("unknown side {:?}", req.side))?;
    let m = market(st, &req.market_id, req.chain_id)?;
    let (chain_id, rpc) = chain(st, m.chain_id)?;
    let price = amount(&req.price, "price")?;
    let quantity = amount(&req.quantity, "quantity")?;
    let tif = kuru::tif(&req.tif).ok_or_else(|| format!("unknown tif {:?}: use gtc, ioc or fok", req.tif))?;
    let user: Address = req.user_wallet.trim().parse().map_err(|_| format!("user_wallet {:?} is not an address", req.user_wallet))?;
    signs_for(st, &req.privy_wallet_id, user).await?;
    let id = trading_account(st, rpc, user).await?;
    let sender = trading_sender(st, &req.privy_wallet_id, user);

    let call = kuru::limit_call(&m, id, side, price, quantity, tif, req.post_only, client_order_id(&req.intent_key))?;

    // a crossing post-only order is skipped in silence on chain: refuse it here
    if req.post_only {
        let (bid, ask) = kuru::best_bid_ask(rpc, sender.from, &m).await?;
        if kuru::crosses(side, price, bid, ask) {
            return Err(format!("post_only {} at {price} crosses the book (bid {bid}, ask {ask}) and would be silently skipped", req.side));
        }
    }
    // a gtc remainder rests in the lowest free slot; read it before, verify after
    let target = match tif {
        kuru::TIF_GTC => kuru::lowest_free_slot(rpc, sender.from, &m, id).await,
        _ => None,
    };
    let notional = kuru::quote_notional(&m, price, quantity);
    // account fee defaults are off today, so the effective rate is read, not assumed
    let (taker_pps, maker_pps) =
        account::effective_fees(rpc, st.account_core, sender.from, id, m.taker_fee_pps, m.maker_fee_pps).await?;

    let hash = submit(st, chain_id, &req.intent_key, &sender, "order", &call, gas::BATCH).await?;
    steps.push(Step::new("order", "submitted", &hash, format!("{} {} @ {price} x {quantity}", m.id, req.side)));

    let base = json!({
        "user_id": id,
        "quote_notional": notional.to_string(),
        "raw_base_atoms": kuru::raw_base_atoms(&m, quantity).to_string(),
        "client_order_id": format!("0x{}", alloy_primitives::hex::encode(client_order_id(&req.intent_key))),
        "maker_fee_pps": maker_pps.to_string(),
        "taker_fee_pps": taker_pps.to_string(),
        "maker_fee_quote": kuru::fee(notional, maker_pps).to_string(),
        "taker_fee_quote": kuru::fee_ceil(notional, taker_pps).to_string(),
    });
    let merge = |extra: Value| {
        let mut v = base.clone();
        if let (Some(a), Some(b)) = (v.as_object_mut(), extra.as_object()) {
            a.extend(b.clone());
        }
        v
    };

    if !mined(st, rpc, &hash, "order").await? {
        return Ok(Done::new(&hash, "submitted", merge(json!({"rested": Value::Null, "reason": "receipt_timeout"}))));
    }
    steps.last_mut().unwrap().status = "confirmed".into();

    // a confirmed tx is not a resting order. read the slot and say what happened.
    let Some(slot) = target else {
        return Ok(Done::new(&hash, "filled_or_expired", merge(json!({"rested": false, "reason": "non-gtc orders never rest"}))));
    };
    match kuru::order_id_at(rpc, sender.from, &m, id, slot).await? {
        0 => Ok(Done::new(
            &hash,
            "not_resting",
            merge(json!({
                "rested": false,
                "slot_idx": slot,
                "reason": if req.post_only { "post_only skipped: it would have crossed" } else { "filled on entry, or skipped" },
            })),
        )),
        order_id => Ok(Done::new(
            &hash,
            "resting",
            merge(json!({"rested": true, "slot_idx": slot, "order_id": order_id.to_string()})),
        )),
    }
}

/// cancel one slot, bound to the order the caller believes is in it.
pub async fn cancel(State(st): State<AppState>, Json(req): Json<CancelRequest>) -> Json<RouteResponse> {
    let (key, wallet) = (req.intent_key.clone(), sender_wallet(&st, &req.privy_wallet_id));
    let inner = st.clone();
    guarded(&st, &key, &wallet, move |mut steps| async move {
        let out = do_cancel(&inner, &req, &mut steps).await;
        (out, steps)
    })
    .await
}

async fn do_cancel(st: &AppState, req: &CancelRequest, steps: &mut Vec<Step>) -> Result<Done, String> {
    let m = market(st, &req.market_id, req.chain_id)?;
    let (chain_id, rpc) = chain(st, m.chain_id)?;
    let expected: u64 = req.expected_order_id.trim().parse().map_err(|_| format!("expected_order_id {:?} is not a uint64", req.expected_order_id))?;
    if expected == 0 {
        return Err("expected_order_id must name the order occupying the slot".into());
    }
    let user: Address = req.user_wallet.trim().parse().map_err(|_| format!("user_wallet {:?} is not an address", req.user_wallet))?;
    signs_for(st, &req.privy_wallet_id, user).await?;
    let id = trading_account(st, rpc, user).await?;
    let sender = trading_sender(st, &req.privy_wallet_id, user);

    let call = kuru::cancel_call(&m, id, req.slot_idx, client_order_id(&req.intent_key))?;

    // cancellation is slot-indexed and slots are reused: a stale id would cancel
    // whatever moved in. ponytail: preflight bind only, batch has no on-chain
    // expectedOrderIds guard - the trading-wallet intent path does.
    let live = kuru::order_id_at(rpc, sender.from, &m, id, req.slot_idx).await?;
    if live != expected {
        return Err(match live {
            0 => format!("slot {} is empty, not order {expected}", req.slot_idx),
            other => format!("slot {} now holds order {other}, not {expected}", req.slot_idx),
        });
    }

    let hash = submit(st, chain_id, &req.intent_key, &sender, "cancel", &call, gas::BATCH).await?;
    steps.push(Step::new("cancel", "submitted", &hash, format!("{} slot {} order {expected}", m.id, req.slot_idx)));
    if !mined(st, rpc, &hash, "cancel").await? {
        return Ok(Done::new(&hash, "submitted", json!({"reason": "receipt_timeout"})));
    }
    steps.last_mut().unwrap().status = "confirmed".into();

    let after = kuru::order_id_at(rpc, sender.from, &m, id, req.slot_idx).await?;
    Ok(Done::new(
        &hash,
        if after == expected { "not_canceled" } else { "canceled" },
        json!({"slot_idx": req.slot_idx, "order_id": expected.to_string(), "slot_now": after.to_string()}),
    ))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::venues::test_registry;

    fn req<T: for<'de> Deserialize<'de>>(json: Value) -> T {
        serde_json::from_value(json).expect("must deserialize")
    }

    fn base() -> Value {
        json!({
            "intent_key": "i1",
            "privy_wallet_id": "w1",
            "user_wallet": "0x1111111111111111111111111111111111111111",
            "market_id": "MON-USDC",
            "venue": "kuru",
            "side": "buy",
            "amount": "1000000",
            "chain_id": 10143
        })
    }

    #[test]
    fn unknown_wire_fields_are_ignored_and_optionals_default() {
        let mut j = base();
        let obj = j.as_object_mut().unwrap();
        obj.insert("to".into(), "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef".into());
        obj.insert("future_field".into(), json!({"nested": [1, 2]}));
        let r: RouteRequest = req(j);
        assert_eq!(r.max_slippage_bps, 0);
        assert_eq!(clamp_slippage(r.max_slippage_bps), 50);
    }

    /// a compromised caller can name a market, never an address.
    #[test]
    fn the_target_comes_from_the_allowlist_not_the_request() {
        let mut j = base();
        let evil = "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef";
        let obj = j.as_object_mut().unwrap();
        for k in ["to", "router", "market_address", "token"] {
            obj.insert(k.into(), evil.into());
        }
        let r: RouteRequest = req(j);

        let m = test_registry().get(&r.market_id).unwrap().clone();
        let call = kuru::swap_call(&m, 7, Side::Buy, U256::from(1u64), U256::ZERO, 0).unwrap();
        assert_eq!(call.to, m.market_address);
        let evil_bytes: Address = evil.parse().unwrap();
        assert!(
            !call.data.windows(20).any(|w| w == evil_bytes.as_slice()),
            "no request-supplied address may reach the calldata"
        );
        // a deposit names a side of the pair; an address never resolves
        let mut jd = base();
        jd["token"] = evil.into();
        let d: DepositRequest = req(jd);
        assert_eq!(m.token(&d.token), None, "only \"base\" or \"quote\" resolve");
        assert_eq!(m.token("quote"), Some(m.quote_token));
    }

    #[test]
    fn an_unlisted_market_never_resolves() {
        let mut j = base();
        j["market_id"] = "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef".into();
        let r: RouteRequest = req(j);
        assert!(test_registry().get(&r.market_id).is_none());
    }

    #[test]
    fn slippage_is_clamped_from_the_request() {
        for (sent, want) in [(0u32, 50u32), (1, 10), (75, 75), (9_999, 300)] {
            let mut j = base();
            j["max_slippage_bps"] = sent.into();
            let r: RouteRequest = req(j);
            assert_eq!(clamp_slippage(r.max_slippage_bps), want);
        }
    }

    #[test]
    fn amounts_are_uint256_safe() {
        let big = "115792089237316195423570985008687907853269984665640564039457584007913129639935";
        assert_eq!(amount(big, "amount").unwrap(), U256::MAX);
        assert!(amount("1e18", "amount").is_err(), "no float or scientific notation");
        assert!(amount("-1", "amount").is_err());
        assert!(amount("0", "amount").unwrap_err().contains("greater than zero"));
        assert!(amount(" 42 ", "amount").is_ok());
    }

    #[test]
    fn an_order_request_carries_venue_native_integers() {
        let o: OrderRequest = req(json!({
            "intent_key": "i2", "privy_wallet_id": "w1",
            "user_wallet": "0x1111111111111111111111111111111111111111",
            "market_id": "WETH-USDC", "side": "buy",
            "price": "275000", "quantity": "5000000000",
            "tif": "gtc", "post_only": true
        }));
        assert_eq!(o.chain_id, 0, "chain_id defaults to the market's");
        let m = test_registry().get(&o.market_id).unwrap().clone();
        let call = kuru::limit_call(
            &m,
            9,
            Side::parse(&o.side).unwrap(),
            amount(&o.price, "price").unwrap(),
            amount(&o.quantity, "quantity").unwrap(),
            kuru::tif(&o.tif).unwrap(),
            o.post_only,
            client_order_id(&o.intent_key),
        )
        .unwrap();
        assert_eq!(call.to, m.market_address);
    }

    #[test]
    fn a_defaulted_tif_is_gtc_and_a_bogus_one_is_refused() {
        let mut j = json!({
            "intent_key": "i3", "privy_wallet_id": "w1",
            "user_wallet": "0x1111111111111111111111111111111111111111",
            "market_id": "WETH-USDC", "side": "sell", "price": "275000", "quantity": "5000000000"
        });
        let o: OrderRequest = req(j.clone());
        assert_eq!(kuru::tif(&o.tif), Some(kuru::TIF_GTC));
        assert!(!o.post_only);
        j["tif"] = "market".into();
        let o: OrderRequest = req(j);
        assert!(kuru::tif(&o.tif).is_none(), "there is no market-order type");
    }

    #[test]
    fn the_client_order_id_is_the_intent_key() {
        assert_eq!(client_order_id("i1"), client_order_id("i1"));
        assert_ne!(client_order_id("i1"), client_order_id("i2"));
        assert_eq!(client_order_id("i1").len(), 32);
    }

    #[test]
    fn response_serializes_with_the_fields_the_wallet_expects() {
        let r = RouteResponse {
            intent_key: "i1".into(),
            tx_hash: "0xaa".into(),
            status: "resting".into(),
            steps: vec![Step::new("order", "confirmed", "0xaa", "WETH-USDC buy".into())],
            error: String::new(),
            info: json!({"rested": true, "slot_idx": 3, "order_id": "190504"}),
        };
        let v = serde_json::to_value(&r).unwrap();
        assert_eq!(v["intent_key"], "i1");
        assert_eq!(v["status"], "resting");
        assert_eq!(v["error"], "");
        assert_eq!(v["steps"][0]["name"], "order");
        assert_eq!(v["info"]["order_id"], "190504");
        // an empty info is omitted entirely
        assert!(serde_json::to_value(RouteResponse::default()).unwrap().get("info").is_none());
    }
}
