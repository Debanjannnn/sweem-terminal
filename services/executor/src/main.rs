use executor::{config::{self, Config, Deployments}, http, policy, privy::{Authorizer, Privy}, rpc::Rpc, store};
use std::sync::Arc;

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt()
        .with_env_filter(tracing_subscriber::EnvFilter::try_from_env("LOG_LEVEL").unwrap_or_else(|_| "info".into()))
        .init();
    if let Err(e) = run().await {
        eprintln!("fatal: {e}");
        std::process::exit(1);
    }
}

async fn run() -> Result<(), String> {
    for f in [".env.executor", ".env", "../../.env.executor", "../../.env"] {
        config::load_dotenv(f);
    }
    let args: Vec<String> = std::env::args().collect();
    if args.get(1).map(String::as_str) == Some("sync-policy") {
        let dep = Deployments::load(&config::require("DEPLOYMENTS_PATH")?)?;
        if args.iter().any(|a| a == "--dry-run") {
            println!("{}", serde_json::to_string_pretty(&policy::rules(&dep)).unwrap());
            return Ok(());
        }
        let privy = Privy::new(config::require("PRIVY_APP_ID")?, &config::require("PRIVY_APP_SECRET")?, Authorizer::new(&config::require("PRIVY_AUTHORIZATION_PRIVATE_KEY")?)?);
        let out = policy::apply(&privy, &dep, config::opt("PRIVY_POLICY_ID"), config::opt("PRIVY_KEY_QUORUM_ID")).await?;
        println!("{}", serde_json::to_string_pretty(&out).unwrap());
        return Ok(());
    }

    let cfg = Config::from_env()?;
    let addr = cfg.addr.clone();
    let privy = Arc::new(Privy::new(cfg.privy_app_id.clone(), &cfg.privy_app_secret, Authorizer::new(&cfg.privy_authorization_key)?));
    let rpc = Arc::new(Rpc::new(cfg.rpc_url.clone()));
    let app = executor::app(cfg, privy, rpc).await?;
    let n = store::fail_interrupted(&app.db).await?;
    if n > 0 {
        tracing::warn!(count = n, "plans interrupted by restart marked failed");
    }
    let listener = tokio::net::TcpListener::bind(&addr).await.map_err(|e| format!("bind {addr}: {e}"))?;
    tracing::info!(%addr, "listening");
    axum::serve(listener, http::router(app))
        .with_graceful_shutdown(async { let _ = tokio::signal::ctrl_c().await; })
        .await
        .map_err(|e| e.to_string())
}
