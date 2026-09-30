use axum::{
    extract::{Path, Request, State},
    http::StatusCode,
    middleware::{self, Next},
    response::{IntoResponse, Response},
    routing::{get, post},
    Json, Router,
};
use serde::Deserialize;
use serde_json::{json, Value};
use std::sync::Arc;

use crate::{config::Deployments, nonce::Leases, privy::Privy, rpc::{Receipt, Rpc}, user};

pub struct AppInner {
    pub db: crate::store::Db,
    pub rpc: Arc<Rpc>,
    pub privy: Arc<Privy>,
    pub dep: Deployments,
    pub secret: String,
    pub max_step_usd: u64,
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
        .route("/api/v1/user/steps", post(user_steps))
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
        "scheduler" | "api" => path == "/api/v1/user/steps" || path.starts_with("/api/v1/tx/"),
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

/// runs the whole plan before answering; 200 carries partial progress when a step fails.
async fn user_steps(State(app): State<App>, body: Result<Json<UserReq>, axum::extract::rejection::JsonRejection>) -> Result<Json<user::Outcome>, Fail> {
    let Json(req) = body.map_err(bad)?;
    let uuid = req.execution_id.len() == 36 && req.execution_id.chars().enumerate().all(|(i, c)| if [8, 13, 18, 23].contains(&i) { c == '-' } else { c.is_ascii_hexdigit() });
    if !uuid {
        return Err(bad("executionId is not a uuid"));
    }
    user::execute(&app, &req.execution_id).await.map(Json)
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
    fn request_fixture_parses() {
        let r: UserReq = serde_json::from_value(fixture("user_steps.request.json")).unwrap();
        assert_eq!(r.execution_id.len(), 36);
    }

    #[test]
    fn responses_match_fixture_shapes() {
        for f in ["user_steps.response.json", "user_steps_failed.response.json"] {
            let o: user::Outcome = serde_json::from_value(fixture(f)).unwrap();
            assert_eq!(serde_json::to_value(&o).unwrap(), fixture(f), "{f}");
        }
        let r = Receipt { status: alloy::primitives::U256::from(1), block_number: alloy::primitives::U256::from(66520000), logs: vec![] };
        let h = "0x9ea4841a00000000000000000000000000000000000000000000000000000001";
        assert_eq!(tx_json(h, Some(&r)), fixture("tx.response.json"));
        assert_eq!(json!({"error": "delegation_inactive"}), fixture("error.response.json"));
    }

    #[test]
    fn caller_allowlist() {
        assert!(allowed("scheduler", "/api/v1/user/steps"));
        assert!(allowed("api", "/api/v1/user/steps"));
        assert!(allowed("scheduler", "/api/v1/tx/0xab"));
        assert!(!allowed("scheduler", "/api/v1/user/mint"));
        assert!(!allowed("seeder", "/api/v1/user/steps"));
        assert!(!allowed("seeder", "/api/v1/tx/0xab"));
        assert!(!allowed("", "/api/v1/tx/0xab"));
    }

    #[test]
    fn constant_time_eq() {
        assert!(eq_ct(b"abc", b"abc"));
        assert!(!eq_ct(b"abc", b"abd"));
        assert!(!eq_ct(b"abc", b"ab"));
    }
}
