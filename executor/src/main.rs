mod auth;
mod config;
mod gas;
mod idem;
mod nonce;
mod perpl_route;
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
    auth::Authorizer,
    config::{Config, Signer},
    idem::Idem,
    nonce::Nonces,
    privy::Privy,
    rpc::Rpc,
    venues::Registry,
};

#[derive(Clone)]
pub struct AppState {
    pub privy: Arc<Privy>,
    pub markets: Arc<Registry>,
    pub rpc: Arc<HashMap<u64, Rpc>>,
    pub nonces: Arc<Nonces>,
    pub idem: Arc<Idem>,
    pub account_core: Address,
    pub signer: Option<Arc<Signer>>,
    pub receipt_timeout: Duration,
    pub perpl: Arc<venues::perpl::Client>,
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
        account_core: cfg.account_core,
        signer: cfg.signer.clone().map(Arc::new),
        receipt_timeout: cfg.receipt_timeout,
        perpl: Arc::new(venues::perpl::Client::new(&cfg.perpl_api_url, cfg.perpl_exchange)),
    };

    let app = Router::new()
        .route("/health", get(health))
        .route("/markets", get(markets_list))
        .route("/account/ensure", post(route::account_ensure))
        .route("/account/deposit", post(route::account_deposit))
        .route("/account/authorize", post(route::account_authorize))
        .route("/account/revoke", post(route::account_revoke))
        .route("/route", post(route::route))
        .route("/order", post(route::order))
        .route("/order/cancel", post(route::cancel))
        .route("/perpl/account", post(perpl_route::account))
        .route("/perpl/deposit", post(perpl_route::deposit))
        .route("/perpl/withdraw", post(perpl_route::withdraw))
        .route("/perpl/order", post(perpl_route::order))
        .route("/perpl/cancel", post(perpl_route::cancel))
        .layer(tower_http::trace::TraceLayer::new_for_http())
        .with_state(state);

    let listener = tokio::net::TcpListener::bind(&cfg.addr).await.map_err(|e| format!("bind {}: {e}", cfg.addr))?;
    info!(addr = %cfg.addr, account_core = %cfg.account_core, signer = ?cfg.signer.as_ref().map(|s| s.address), "executor listening");

    axum::serve(listener, app).with_graceful_shutdown(shutdown()).await.map_err(|e| e.to_string())
}

async fn health(State(st): State<AppState>) -> Json<serde_json::Value> {
    Json(json!({
        "status": "ok",
        "markets": st.markets.len(),
        "chains": st.rpc.keys().copied().collect::<Vec<_>>(),
        "account_core": st.account_core,
        "signer": st.signer.as_ref().map(|s| s.address),
        "perpl_exchange": st.perpl.pinned,
    }))
}

async fn markets_list(State(st): State<AppState>) -> Json<serde_json::Value> {
    let markets = st.markets.all();
    Json(json!({ "count": markets.len(), "account_core": st.account_core, "markets": markets }))
}

async fn shutdown() {
    let _ = tokio::signal::ctrl_c().await;
    info!("shutting down");
}
