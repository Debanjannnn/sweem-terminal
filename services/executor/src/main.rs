use executor::{config::{self, Config}, http, policy, privy::{Authorizer, Privy}, rpc::Rpc, user};
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
    let cfg = Config::from_env()?;
    let addr = cfg.addr.clone();
    let privy = Arc::new(Privy::new(cfg.privy_app_id.clone(), &cfg.privy_app_secret, Authorizer::new(&cfg.privy_authorization_key)?));
    let rpc = Arc::new(Rpc::new(cfg.rpc_url.clone()));

    let (policy_id, quorum) = (config::opt("PRIVY_POLICY_ID"), config::opt("PRIVY_KEY_QUORUM_ID"));
    if std::env::args().nth(1).as_deref() == Some("sync-policy") {
        let b = policy::baskets(&rpc, &cfg.deployments).await?;
        let out = policy::apply(&privy, &cfg.deployments, &b, policy_id, quorum).await?;
        println!("{}", serde_json::to_string_pretty(&out).unwrap());
        return Ok(());
    }
    tokio::spawn(policy::watch(privy.clone(), rpc.clone(), cfg.deployments.clone(), policy_id, quorum));

    let app = executor::app(cfg, privy, rpc).await?;
    user::resume(&app).await?;
    let listener = tokio::net::TcpListener::bind(&addr).await.map_err(|e| format!("bind {addr}: {e}"))?;
    tracing::info!(%addr, bot = %app.bot.address, "listening");
    axum::serve(listener, http::router(app))
        .with_graceful_shutdown(async { let _ = tokio::signal::ctrl_c().await; })
        .await
        .map_err(|e| e.to_string())
}
