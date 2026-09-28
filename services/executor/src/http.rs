use alloy::primitives::{Address, U256};
use axum::{
    extract::{Path, Request, State},
    http::StatusCode,
    middleware::{self, Next},
    response::{IntoResponse, Response},
    routing::{get, post},
    Json, Router,
};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::sync::Arc;

use crate::{bot::Bot, config::Deployments, contracts, nonce::Leases, privy::Privy, rpc::{Receipt, Rpc}, user};

pub struct AppInner {
    pub db: crate::store::Db,
    pub rpc: Arc<Rpc>,
    pub privy: Arc<Privy>,
    pub bot: Bot,
    pub dep: Deployments,
    pub secret: String,
    pub leases: Leases,
}

pub type App = Arc<AppInner>;

pub struct Fail(pub StatusCode, pub &'static str);

impl IntoResponse for Fail {
    fn into_response(self) -> Response {
        (self.0, Json(json!({ "error": self.1 }))).into_response()
    }
}

fn bad(e: impl std::fmt::Display) -> Fail {
    tracing::debug!(error = %e, "bad request");
    Fail(StatusCode::BAD_REQUEST, "bad_request")
}

fn chain(e: impl std::fmt::Display) -> Fail {
    tracing::warn!(error = %e, "chain");
    Fail(StatusCode::BAD_GATEWAY, "chain_error")
}

pub fn router(app: App) -> Router {
    let api = Router::new()
        .route("/api/v1/user/mint", post(|s, b| user_route(s, b, "mint")))
        .route("/api/v1/user/redeem", post(|s, b| user_route(s, b, "redeem")))
        .route("/api/v1/bot/orders", post(bot_orders))
        .route("/api/v1/bot/deposit", post(bot_deposit))
        .route("/api/v1/bot/balances", get(bot_balances))
        .route("/api/v1/tx/{hash}", get(tx))
        .route_layer(middleware::from_fn_with_state(app.clone(), guard));
    Router::new()
        .route("/health", get(|| async { Json(json!({"status": "ok"})) }))
        .route("/healthz", get(healthz))
        .merge(api)
        .with_state(app)
}

/// which X-Caller may reach which path.
pub fn allowed(caller: &str, path: &str) -> bool {
    match caller {
        "scheduler" => path.starts_with("/api/v1/user/") || path.starts_with("/api/v1/tx/"),
        "seeder" => path.starts_with("/api/v1/bot/") || path.starts_with("/api/v1/tx/"),
        _ => false,
    }
}

fn eq_ct(a: &[u8], b: &[u8]) -> bool {
    a.len() == b.len() && a.iter().zip(b).fold(0u8, |acc, (x, y)| acc | (x ^ y)) == 0
}

async fn guard(State(app): State<App>, req: Request, next: Next) -> Response {
    let ok = {
        let h = |k: &str| req.headers().get(k).and_then(|v| v.to_str().ok()).unwrap_or_default();
        eq_ct(h("x-internal-secret").as_bytes(), app.secret.as_bytes()) && allowed(h("x-caller"), req.uri().path())
    };
    if !ok {
        return Fail(StatusCode::UNAUTHORIZED, "unauthorized").into_response();
    }
    next.run(req).await
}

async fn healthz(State(app): State<App>) -> Response {
    let db = match app.db.get().await { Ok(c) => c.simple_query("select 1").await.is_ok(), Err(_) => false };
    let rpc = app.rpc.block_number().await.is_ok();
    let code = if db && rpc { StatusCode::OK } else { StatusCode::SERVICE_UNAVAILABLE };
    (code, Json(json!({"db": db, "rpc": rpc}))).into_response()
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct UserReq {
    pub execution_id: String,
}

#[derive(Serialize, Deserialize, Debug, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct UserResp {
    pub execution_id: String,
    pub status: String,
    pub tx_hash: String,
}

async fn user_route(State(app): State<App>, body: Result<Json<UserReq>, axum::extract::rejection::JsonRejection>, kind: &'static str) -> Result<Json<UserResp>, Fail> {
    let Json(req) = body.map_err(bad)?;
    let uuid = req.execution_id.len() == 36 && req.execution_id.chars().enumerate().all(|(i, c)| if [8, 13, 18, 23].contains(&i) { c == '-' } else { c.is_ascii_hexdigit() });
    if !uuid {
        return Err(bad("executionId is not a uuid"));
    }
    let hash = user::execute(&app, &req.execution_id, kind).await?;
    Ok(Json(UserResp { execution_id: req.execution_id, status: "submitted".into(), tx_hash: hash }))
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub struct OrdersReq {
    pub market: Address,
    pub cancels: Vec<String>,
    pub orders: Vec<contracts::OrderReq>,
}

#[derive(Serialize, Deserialize, Debug)]
#[serde(rename_all = "camelCase")]
pub struct TxResp {
    pub tx_hash: String,
    pub status: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub order_ids: Option<Vec<String>>,
}

async fn bot_orders(State(app): State<App>, body: Result<Json<OrdersReq>, axum::extract::rejection::JsonRejection>) -> Result<Json<TxResp>, Fail> {
    let Json(req) = body.map_err(bad)?;
    if !app.dep.kuru.markets.values().any(|m| *m == req.market) {
        return Err(bad("unknown market"));
    }
    let cancels = req.cancels.iter().map(|c| c.parse::<u64>()).collect::<Result<Vec<_>, _>>().map_err(bad)?;
    let orders = req.orders.iter().map(contracts::native_order).collect::<Result<Vec<_>, _>>().map_err(bad)?;
    if cancels.is_empty() && orders.is_empty() {
        return Err(bad("nothing to do"));
    }
    let p = app.bot.place(&app.rpc, &app.dep, req.market, &cancels, orders).await.map_err(chain)?;
    Ok(Json(TxResp { tx_hash: p.hash.map(|h| h.to_string()).unwrap_or_default(), status: p.status.into(), order_ids: Some(p.order_ids) }))
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub struct DepositReq {
    pub token: Address,
    pub amount: String,
}

async fn bot_deposit(State(app): State<App>, body: Result<Json<DepositReq>, axum::extract::rejection::JsonRejection>) -> Result<Json<TxResp>, Fail> {
    let Json(req) = body.map_err(bad)?;
    let amount = contracts::u256_str(&req.amount).map_err(bad)?;
    if amount.is_zero() {
        return Err(bad("zero amount"));
    }
    let h = app.bot.deposit(&app.rpc, app.dep.kuru.account_core, req.token, amount).await.map_err(chain)?;
    Ok(Json(TxResp { tx_hash: h.to_string(), status: "submitted".into(), order_ids: None }))
}

async fn bot_balances(State(app): State<App>) -> Result<Json<Value>, Fail> {
    let id = app.bot.account_id(&app.rpc, app.dep.kuru.account_core).await.map_err(chain)?;
    let b = app.bot.balances(&app.rpc, &app.dep).await.map_err(chain)?;
    Ok(Json(balances_json(id, &b)))
}

fn balances_json(id: u64, b: &[(Address, U256, U256)]) -> Value {
    let balances: Vec<Value> = b
        .iter()
        .map(|(t, free, locked)| json!({"token": t.to_string().to_lowercase(), "free": free.to_string(), "locked": locked.to_string()}))
        .collect();
    json!({"accountId": id.to_string(), "balances": balances})
}

async fn tx(State(app): State<App>, Path(hash): Path<String>) -> Result<Json<Value>, Fail> {
    if hash.len() != 66 || !hash.starts_with("0x") || !hash[2..].bytes().all(|b| b.is_ascii_hexdigit()) {
        return Err(bad("hash"));
    }
    Ok(Json(tx_json(&hash, app.rpc.receipt(&hash).await.map_err(chain)?.as_ref())))
}

fn tx_json(hash: &str, r: Option<&Receipt>) -> Value {
    match r {
        None => json!({"txHash": hash, "status": "submitted", "blockNumber": null, "error": null}),
        Some(r) => json!({
            "txHash": hash,
            "status": if r.ok() { "confirmed" } else { "failed" },
            "blockNumber": r.block_number.to_string(),
            "error": if r.ok() { Value::Null } else { json!("reverted") },
        }),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn fixture(name: &str) -> Value {
        let p = format!("{}/../../testdata/contracts/executor/{name}", env!("CARGO_MANIFEST_DIR"));
        serde_json::from_str(&std::fs::read_to_string(p).unwrap()).unwrap()
    }

    #[test]
    fn request_fixtures_parse() {
        for f in ["user_mint.request.json", "user_redeem.request.json"] {
            serde_json::from_value::<UserReq>(fixture(f)).unwrap();
        }
        let o: OrdersReq = serde_json::from_value(fixture("bot_orders.request.json")).unwrap();
        assert_eq!(o.cancels, vec!["12", "13"]);
        assert!(o.orders.iter().all(|x| contracts::native_order(x).is_ok()));
        serde_json::from_value::<DepositReq>(fixture("bot_deposit.request.json")).unwrap();
    }

    #[test]
    fn responses_match_fixture_shapes() {
        let u: UserResp = serde_json::from_value(fixture("user_tx.response.json")).unwrap();
        assert_eq!(serde_json::to_value(&u).unwrap(), fixture("user_tx.response.json"));

        let d: TxResp = serde_json::from_value(fixture("bot_deposit.response.json")).unwrap();
        assert_eq!(serde_json::to_value(&d).unwrap(), fixture("bot_deposit.response.json"));
        for f in ["bot_orders.response.json", "bot_orders_noop.response.json"] {
            let o: TxResp = serde_json::from_value(fixture(f)).unwrap();
            assert!(o.order_ids.is_some(), "{f}");
            assert_eq!(serde_json::to_value(&o).unwrap(), fixture(f));
        }

        let r = Receipt { status: U256::from(1), block_number: U256::from(66520000), logs: vec![] };
        let h = "0x9ea4841a00000000000000000000000000000000000000000000000000000001";
        assert_eq!(tx_json(h, Some(&r)), fixture("tx.response.json"));

        let usdc: Address = "0xee0722ead54f1b4fe97be399be43bc0226a6f97e".parse().unwrap();
        let b = [(usdc, U256::from(5_000_000_000u64), U256::from(1_000_000_000u64)), (Address::ZERO, U256::from(10u64).pow(U256::from(20)), U256::ZERO)];
        assert_eq!(balances_json(42, &b), fixture("bot_balances.response.json"));

        let err = serde_json::to_value(json!({"error": "delegation_inactive"})).unwrap();
        assert_eq!(err, fixture("error.response.json"));
    }

    #[test]
    fn caller_allowlist() {
        assert!(allowed("scheduler", "/api/v1/user/mint"));
        assert!(allowed("scheduler", "/api/v1/tx/0xab"));
        assert!(!allowed("scheduler", "/api/v1/bot/orders"));
        assert!(allowed("seeder", "/api/v1/bot/deposit"));
        assert!(allowed("seeder", "/api/v1/tx/0xab"));
        assert!(!allowed("seeder", "/api/v1/user/redeem"));
        assert!(!allowed("api", "/api/v1/tx/0xab"));
        assert!(!allowed("", "/api/v1/tx/0xab"));
    }

    #[test]
    fn constant_time_eq() {
        assert!(eq_ct(b"abc", b"abc"));
        assert!(!eq_ct(b"abc", b"abd"));
        assert!(!eq_ct(b"abc", b"ab"));
    }
}
