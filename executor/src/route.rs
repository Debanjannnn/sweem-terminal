use alloy_primitives::{Address, U256};
use axum::{extract::State, Json};
use serde::{Deserialize, Serialize};
use tracing::{info, warn};

use crate::{
    gas, idem,
    rpc::{self, Outcome},
    venues::{clamp_slippage, kuru, kuru::Swap, Call, Market, Side, Venue},
    AppState,
};

#[derive(Debug, Clone, Deserialize)]
pub struct RouteRequest {
    pub intent_key: String,
    pub privy_wallet_id: String,
    pub user_wallet: String,
    /// a name, never an address: the target is resolved from the allowlist
    pub market_id: String,
    pub venue: String,
    pub side: String,
    pub amount: String,
    #[serde(default)]
    pub max_slippage_bps: u32,
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
    fn new(name: &str, status: &str, tx_hash: &str, detail: String) -> Self {
        Self { name: name.into(), status: status.into(), tx_hash: tx_hash.into(), detail }
    }
}

struct Plan {
    market: Market,
    swap: Swap,
    amount: U256,
    slippage_bps: u32,
    user: Address,
}

pub async fn route(State(st): State<AppState>, Json(req): Json<RouteRequest>) -> Json<RouteResponse> {
    let key = req.intent_key.clone();
    if let Some(done) = st.idem.get(&key) {
        info!(intent_key = %key, "replaying recorded result");
        return Json(done);
    }

    // serialized per wallet: privy assigns the nonce
    let _lease = st.nonces.lease(&req.privy_wallet_id).await;
    if let Some(done) = st.idem.get(&key) {
        return Json(done);
    }

    let mut steps = Vec::new();
    let resp = match execute(&st, &req, &mut steps).await {
        Ok(hash) => RouteResponse { intent_key: key.clone(), tx_hash: hash, status: "submitted".into(), steps, error: String::new() },
        Err(e) => {
            warn!(intent_key = %key, error = %e, "route failed");
            RouteResponse { intent_key: key.clone(), tx_hash: String::new(), status: "failed".into(), steps, error: e }
        }
    };
    st.idem.put(&key, &resp);
    Json(resp)
}

async fn execute(st: &AppState, req: &RouteRequest, steps: &mut Vec<Step>) -> Result<String, String> {
    let p = validate(st, req).await?;
    let rpc = st.rpc.get(&p.market.chain_id).ok_or("no rpc for chain")?;

    if !p.swap.native {
        if let Some(call) = kuru::approve_if_needed(rpc, p.swap.debit, st.router, p.user, p.amount).await {
            let hash = submit(st, req, "approve", &call, gas::APPROVE, &p).await?;
            steps.push(Step::new("approve", "submitted", &hash, p.swap.debit.to_string()));
            // the swap is simulated against this allowance, so it has to land first
            match rpc::wait(|| rpc.receipt(&hash), st.receipt_timeout, rpc::POLL_INTERVAL).await {
                Some(Outcome::Confirmed) => steps.last_mut().unwrap().status = "confirmed".into(),
                Some(Outcome::Reverted) => return Err(format!("approve reverted: {hash}")),
                None => return Err(format!("approve not mined within {:?}: {hash}", st.receipt_timeout)),
            }
        }
    }

    // refuse to swap without a slippage bound rather than submit an unprotected one
    let quoted = kuru::quote(rpc, st.router, p.user, &p.market, &p.swap, p.amount)
        .await
        .map_err(|e| format!("quote simulation failed: {e}"))?;
    let min_out = kuru::min_out(quoted, p.slippage_bps);
    steps.push(Step::new("quote", "ok", "", format!("out={quoted} min_out={min_out} bps={}", p.slippage_bps)));

    let call = kuru::swap_call(st.router, &p.market, &p.swap, p.amount, min_out);
    let hash = submit(st, req, "swap", &call, gas::SWAP, &p).await?;
    steps.push(Step::new("swap", "submitted", &hash, p.market.id.clone()));
    Ok(hash)
}

async fn validate(st: &AppState, req: &RouteRequest) -> Result<Plan, String> {
    if Venue::parse(&req.venue).is_none() {
        return Err(format!("unknown venue {:?}", req.venue));
    }
    let side = Side::parse(&req.side).ok_or_else(|| format!("unknown side {:?}", req.side))?;
    // the only source of a callable address
    let market = st.markets.get(&req.market_id).ok_or_else(|| format!("market {:?} is not on the allowlist", req.market_id))?.clone();
    if req.chain_id != 0 && req.chain_id != market.chain_id {
        return Err(format!("market {} is on chain {}, not {}", market.id, market.chain_id, req.chain_id));
    }
    let amount = U256::from_str_radix(req.amount.trim(), 10).map_err(|_| format!("amount {:?} is not a uint256", req.amount))?;
    if amount.is_zero() {
        return Err("amount must be greater than zero".into());
    }
    let user: Address = req.user_wallet.trim().parse().map_err(|_| format!("user_wallet {:?} is not an address", req.user_wallet))?;

    // the wallet id is what actually signs: it must be the address we priced for
    let owner = st.privy.get_wallet(&req.privy_wallet_id).await.map_err(|e| format!("privy wallet lookup failed: {e}"))?;
    let owner: Address = owner.address.parse().map_err(|_| format!("privy returned a bad address for {}", req.privy_wallet_id))?;
    if owner != user {
        return Err(format!("privy wallet {} is {owner}, not {user}", req.privy_wallet_id));
    }

    Ok(Plan { swap: kuru::plan(&market, side), market, amount, slippage_bps: clamp_slippage(req.max_slippage_bps), user })
}

async fn submit(st: &AppState, req: &RouteRequest, step: &str, call: &Call, fallback_gas: u64, p: &Plan) -> Result<String, String> {
    let rpc = st.rpc.get(&p.market.chain_id).ok_or("no rpc for chain")?;
    let limit = gas::limit(rpc, p.user, call.to, call.value, &call.data, fallback_gas).await;
    let fees = gas::fees(rpc, limit).await;
    gas::affordable(rpc, p.user, &fees, call.value).await?;
    let tx = st
        .privy
        .send_transaction(
            &req.privy_wallet_id,
            p.market.chain_id,
            call,
            &fees,
            &idem::privy_key(&req.intent_key, step),
            &idem::reference_id(&req.intent_key, step),
        )
        .await
        .map_err(|e| format!("{step} failed: {e}"))?;
    info!(step, hash = %tx.hash, gas_limit = fees.gas_limit, "submitted");
    Ok(tx.hash)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::venues::test_registry;

    const ROUTER: Address = crate::config::KURU_ROUTER;

    fn req(json: serde_json::Value) -> RouteRequest {
        serde_json::from_value(json).expect("must deserialize")
    }

    fn base() -> serde_json::Value {
        serde_json::json!({
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
        j["max_slippage_bps"] = serde_json::Value::Null;
        let obj = j.as_object_mut().unwrap();
        obj.remove("max_slippage_bps");
        obj.insert("to".into(), "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef".into());
        obj.insert("future_field".into(), serde_json::json!({"nested": [1, 2]}));
        let r = req(j);
        assert_eq!(r.max_slippage_bps, 0);
        assert_eq!(clamp_slippage(r.max_slippage_bps), 50);
    }

    /// a compromised caller can name a market, never an address.
    #[test]
    fn the_target_comes_from_the_allowlist_not_the_request() {
        let mut j = base();
        let evil = "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef";
        let obj = j.as_object_mut().unwrap();
        obj.insert("to".into(), evil.into());
        obj.insert("router".into(), evil.into());
        obj.insert("market_address".into(), evil.into());
        let r = req(j);

        let m = test_registry().get(&r.market_id).unwrap().clone();
        let call = kuru::swap_call(ROUTER, &m, &kuru::plan(&m, Side::Buy), U256::from(1u64), U256::ZERO);
        assert_eq!(call.to, ROUTER);
        let evil_bytes: Address = evil.parse().unwrap();
        assert!(
            !call.data.windows(20).any(|w| w == evil_bytes.as_slice()),
            "no request-supplied address may reach the calldata"
        );
        assert!(call.data.windows(20).any(|w| w == m.market_address.as_slice()));
    }

    #[test]
    fn an_unlisted_market_never_resolves() {
        let mut j = base();
        j["market_id"] = "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef".into();
        assert!(test_registry().get(&req(j).market_id).is_none());
    }

    #[test]
    fn slippage_is_clamped_from_the_request() {
        for (sent, want) in [(0u32, 50u32), (1, 10), (75, 75), (9_999, 300)] {
            let mut j = base();
            j["max_slippage_bps"] = sent.into();
            assert_eq!(clamp_slippage(req(j).max_slippage_bps), want);
        }
    }

    #[test]
    fn amount_is_uint256_safe() {
        let big = "115792089237316195423570985008687907853269984665640564039457584007913129639935";
        assert_eq!(U256::from_str_radix(big, 10).unwrap(), U256::MAX);
        assert!(U256::from_str_radix("1e18", 10).is_err(), "no float or scientific notation");
        assert!(U256::from_str_radix("-1", 10).is_err());
    }

    #[test]
    fn response_serializes_with_the_fields_the_wallet_expects() {
        let r = RouteResponse {
            intent_key: "i1".into(),
            tx_hash: "0xaa".into(),
            status: "submitted".into(),
            steps: vec![Step::new("swap", "submitted", "0xaa", "MON-USDC".into())],
            error: String::new(),
        };
        let v = serde_json::to_value(&r).unwrap();
        assert_eq!(v["intent_key"], "i1");
        assert_eq!(v["status"], "submitted");
        assert_eq!(v["error"], "");
        assert_eq!(v["steps"][0]["name"], "swap");
        assert!(v["steps"].is_array());
    }
}
