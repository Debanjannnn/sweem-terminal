mod auth;
mod config;
mod gas;
mod idem;
mod nonce;
mod privy;
mod route;
mod rpc;
mod venues;

use alloy_primitives::Address;
use axum::{
    extract::State,
    routing::{get, post},
    Json, Router,
};
use serde_json::json;
use std::{collections::HashMap, sync::Arc, time::Duration};
use tracing::info;

use crate::{
    auth::Authorizer, config::Config, idem::Idem, nonce::Nonces, privy::Privy, rpc::Rpc,
    venues::Registry,
};

#[derive(Clone)]
pub struct AppState {
    pub privy: Arc<Privy>,
    pub markets: Arc<Registry>,
    pub rpc: Arc<HashMap<u64, Rpc>>,
    pub nonces: Arc<Nonces>,
    pub idem: Arc<Idem>,
    pub router: Address,
    pub receipt_timeout: Duration,
}

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "executor=info,tower_http=info".into()),
        )
        .init();

    if let Err(e) = run().await {
        eprintln!("fatal: {e}");
        std::process::exit(1);
    }
}

async fn run() -> Result<(), String> {
    let cfg = Config::from_env()?;
    let markets = Registry::load(&cfg.markets_path)?;
    info!(count = markets.len(), path = %cfg.markets_path, "market allowlist loaded");

    let authorizer = match cfg.privy_authorization_key.as_deref() {
        Some(k) => Some(Authorizer::new(k).map_err(|e| format!("PRIVY_AUTHORIZATION_PRIVATE_KEY: {e}"))?),
        None => {
            tracing::warn!("PRIVY_AUTHORIZATION_PRIVATE_KEY not set - wallets owned by a key quorum will reject every request");
            None
        }
    };

    let state = AppState {
        privy: Arc::new(Privy::new(cfg.privy_app_id, cfg.privy_app_secret, authorizer)),
        markets: Arc::new(markets),
        rpc: Arc::new(cfg.rpc_urls.into_iter().map(|(id, url)| (id, Rpc::new(url))).collect()),
        nonces: Arc::default(),
        idem: Arc::default(),
        router: cfg.kuru_router,
        receipt_timeout: cfg.receipt_timeout,
    };

    let app = Router::new()
        .route("/health", get(health))
        .route("/markets", get(markets_list))
        .route("/route", post(route::route))
        .layer(tower_http::trace::TraceLayer::new_for_http())
        .with_state(state);

    let listener = tokio::net::TcpListener::bind(&cfg.addr).await.map_err(|e| format!("bind {}: {e}", cfg.addr))?;
    info!(addr = %cfg.addr, router = %cfg.kuru_router, "executor listening");

    axum::serve(listener, app).with_graceful_shutdown(shutdown()).await.map_err(|e| e.to_string())
}

async fn health(State(st): State<AppState>) -> Json<serde_json::Value> {
    Json(json!({
        "status": "ok",
        "markets": st.markets.len(),
        "chains": st.rpc.keys().copied().collect::<Vec<_>>(),
    }))
}

async fn markets_list(State(st): State<AppState>) -> Json<serde_json::Value> {
    let markets = st.markets.all();
    Json(json!({ "count": markets.len(), "router": st.router, "markets": markets }))
}

async fn shutdown() {
    let _ = tokio::signal::ctrl_c().await;
    info!("shutting down");
}
