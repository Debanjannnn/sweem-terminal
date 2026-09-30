use anyhow::{Context, Result};
use serde_json::Value;
use std::collections::HashMap;
use std::env;

/// Contract address (lowercase) -> (venue, underlying asset address or "").
pub type Contracts = HashMap<String, (String, String)>;

pub struct Config {
    pub database_url: String,
    pub hypersync_url: String,
    pub envio_token: String,
    pub finality_blocks: u64,
    pub port: u16,
    pub start_block: u64,
    pub markets: Vec<String>,
    /// Aave-style pools (Supply/Withdraw).
    pub pools: Contracts,
    /// ERC-4626 vaults incl. shMON.
    pub vaults: Contracts,
    pub active_vault: String,
    pub perpl: String,
    pub usdc: String,
    /// Empty until a treasury is set in deployments.
    pub treasury: String,
}

fn var(k: &str, default: &str) -> String {
    env::var(k).unwrap_or_else(|_| default.to_string())
}

fn s(v: &Value) -> Result<String> {
    Ok(v.as_str()
        .with_context(|| format!("not a string: {v}"))?
        .to_lowercase())
}

impl Config {
    pub fn from_env() -> Result<Self> {
        let path = var("DEPLOYMENTS_PATH", "/deployments/143.json");
        let d: Value =
            serde_json::from_str(&std::fs::read_to_string(&path).context(path.clone())?)?;
        let token = |sym: &Value| s(&d["tokens"][sym.as_str().unwrap_or_default()]["address"]);
        let mut pools = Contracts::new();
        for v in ["aaveV3", "neverland"] {
            let name = if v == "aaveV3" { "aave" } else { v };
            pools.insert(s(&d["lending"][v]["pool"])?, (name.into(), String::new()));
        }
        let mut vaults = Contracts::new();
        for v in d["lending"]["erc4626"].as_array().context("erc4626")? {
            vaults.insert(s(&v["vault"])?, (s(&v["protocol"])?, token(&v["asset"])?));
        }
        vaults.insert(
            s(&d["staking"]["shmonad"])?,
            ("shmonad".into(), s(&d["tokens"]["MON"]["address"])?),
        );
        Ok(Config {
            database_url: env::var("DATABASE_URL").context("DATABASE_URL")?,
            hypersync_url: var("HYPERSYNC_URL", "https://monad.hypersync.xyz"),
            envio_token: env::var("ENVIO_API_TOKEN").context("ENVIO_API_TOKEN")?,
            // Monad RPC `finalized` tag trails latest by 2 blocks (MonadBFT); 3 adds one block of margin.
            finality_blocks: var("FINALITY_BLOCKS", "3").parse()?,
            port: var("PORT", "8085").parse()?,
            start_block: d["startBlock"].as_u64().context("deployments.startBlock")?,
            markets: d["kuru"]["markets"]
                .as_object()
                .context("kuru.markets")?
                .values()
                .map(|m| s(&m["orderbook"]))
                .collect::<Result<_>>()?,
            pools,
            vaults,
            active_vault: s(&d["kuru"]["activeVault"])?,
            perpl: s(&d["perpl"]["exchange"])?,
            usdc: s(&d["tokens"]["USDC"]["address"])?,
            treasury: s(&d["treasury"]).unwrap_or_default(),
        })
    }
}
