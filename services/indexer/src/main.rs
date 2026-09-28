use indexer::{config::Config, health, Indexer};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Duration;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let cfg = Config::from_env()?;
    let last_ok = Arc::new(AtomicU64::new(0));
    tokio::spawn(health::serve(
        cfg.port,
        cfg.database_url.clone(),
        last_ok.clone(),
    ));
    let mut ix = Indexer::new(&cfg, "main").await?;
    loop {
        match ix.step().await {
            Ok(more) => {
                last_ok.store(health::now(), Ordering::Relaxed);
                if more {
                    continue;
                }
            }
            Err(e) => eprintln!("indexer step: {e:#}"),
        }
        // HyperSync token budget is 30 req/min; one step = one query
        tokio::time::sleep(Duration::from_secs(5)).await;
    }
}
