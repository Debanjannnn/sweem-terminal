use anyhow::{Context, Result};
use std::env;

pub struct Config {
    pub database_url: String,
    pub hypersync_url: String,
    pub envio_token: String,
    pub finality_blocks: u64,
    pub port: u16,
    pub factory: String,
    pub start_block: u64,
}

fn var(k: &str, default: &str) -> String {
    env::var(k).unwrap_or_else(|_| default.to_string())
}

impl Config {
    pub fn from_env() -> Result<Self> {
        let path = var("DEPLOYMENTS_PATH", "/deployments/10143.json");
        let d: serde_json::Value =
            serde_json::from_str(&std::fs::read_to_string(&path).context(path.clone())?)?;
        Ok(Config {
            database_url: env::var("DATABASE_URL").context("DATABASE_URL")?,
            hypersync_url: var("HYPERSYNC_URL", "https://monad-testnet.hypersync.xyz"),
            envio_token: env::var("ENVIO_API_TOKEN").context("ENVIO_API_TOKEN")?,
            // Monad RPC `finalized` tag trails latest by 2 blocks (MonadBFT); 3 adds one block of margin.
            finality_blocks: var("FINALITY_BLOCKS", "3").parse()?,
            port: var("PORT", "8085").parse()?,
            factory: d["factory"]
                .as_str()
                .context("deployments.factory")?
                .to_lowercase(),
            start_block: d["startBlock"].as_u64().context("deployments.startBlock")?,
        })
    }
}
