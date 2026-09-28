use alloy::primitives::Address;
use serde::Deserialize;
use std::{collections::BTreeMap, env};

#[derive(Debug, Clone, Deserialize)]
pub struct Kuru {
    #[serde(rename = "accountCore")]
    pub account_core: Address,
    pub usdc: Address,
    pub markets: BTreeMap<String, Address>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Deployments {
    #[serde(rename = "chainId")]
    pub chain_id: u64,
    pub factory: Address,
    pub kuru: Kuru,
}

impl Deployments {
    pub fn load(path: &str) -> Result<Self, String> {
        let raw = std::fs::read_to_string(path).map_err(|e| format!("{path}: {e}"))?;
        serde_json::from_str(&raw).map_err(|e| format!("{path}: {e}"))
    }
}

pub struct Config {
    pub addr: String,
    pub rpc_url: String,
    pub database_url: String,
    pub internal_secret: String,
    pub privy_app_id: String,
    pub privy_app_secret: String,
    pub privy_authorization_key: String,
    pub bot_private_key: String,
    pub deployments: Deployments,
}

pub fn opt(key: &str) -> Option<String> {
    env::var(key).ok().map(|v| v.trim().to_string()).filter(|v| !v.is_empty())
}

pub fn require(key: &str) -> Result<String, String> {
    opt(key).ok_or_else(|| format!("missing env {key}"))
}

impl Config {
    pub fn from_env() -> Result<Self, String> {
        let deployments = Deployments::load(&require("DEPLOYMENTS_PATH")?)?;
        if deployments.chain_id != 10143 {
            return Err(format!("deployments are for chain {}, executor signs on 10143 only", deployments.chain_id));
        }
        Ok(Self {
            addr: opt("EXECUTOR_ADDR").unwrap_or_else(|| "0.0.0.0:8083".into()),
            rpc_url: require("RPC_URL")?,
            database_url: require("DATABASE_URL")?,
            internal_secret: require("INTERNAL_SECRET")?,
            privy_app_id: require("PRIVY_APP_ID")?,
            privy_app_secret: require("PRIVY_APP_SECRET")?,
            privy_authorization_key: require("PRIVY_AUTHORIZATION_PRIVATE_KEY")?,
            bot_private_key: require("BOT_PRIVATE_KEY")?,
            deployments,
        })
    }
}

/// local runs: `KEY=value` lines, never overriding the real environment.
pub fn load_dotenv(path: &str) {
    let Ok(body) = std::fs::read_to_string(path) else { return };
    for line in body.lines().map(str::trim).filter(|l| !l.is_empty() && !l.starts_with('#')) {
        if let Some((k, v)) = line.split_once('=') {
            if env::var(k.trim()).is_err() {
                unsafe { env::set_var(k.trim(), v.trim()) };
            }
        }
    }
}
