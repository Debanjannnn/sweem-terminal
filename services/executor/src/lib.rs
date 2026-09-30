pub mod config;
pub mod contracts;
pub mod gas;
pub mod http;
pub mod nonce;
pub mod policy;
pub mod privy;
pub mod rpc;
pub mod signer;
pub mod steps;
pub mod store;
pub mod user;

use std::sync::Arc;

pub async fn app(cfg: config::Config, privy: Arc<privy::Privy>, rpc: Arc<rpc::Rpc>) -> Result<http::App, String> {
    Ok(Arc::new(http::AppInner {
        db: store::pool(&cfg.database_url)?,
        rpc,
        privy,
        dep: cfg.deployments,
        secret: cfg.internal_secret,
        max_step_usd: cfg.max_step_usd,
        leases: Default::default(),
    }))
}

/// tests and local runs: env from main/.env*, deployments by absolute path.
pub async fn app_from_env() -> Result<http::App, String> {
    let root = concat!(env!("CARGO_MANIFEST_DIR"), "/../..");
    if config::opt("DEPLOYMENTS_PATH").is_none_or(|p| !std::path::Path::new(&p).exists()) {
        unsafe { std::env::set_var("DEPLOYMENTS_PATH", format!("{root}/contracts/deployments/143.json")) };
    }
    config::load_dotenv(&format!("{root}/.env.executor"));
    config::load_dotenv(&format!("{root}/.env"));
    let cfg = config::Config::from_env()?;
    let privy = Arc::new(privy::Privy::new(cfg.privy_app_id.clone(), &cfg.privy_app_secret, privy::Authorizer::new(&cfg.privy_authorization_key)?));
    let rpc = Arc::new(rpc::Rpc::new(cfg.rpc_url.clone()));
    app(cfg, privy, rpc).await
}
