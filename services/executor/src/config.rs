use alloy::primitives::Address;
use serde::Deserialize;
use std::{collections::BTreeMap, env};

#[derive(Debug, Clone, Deserialize)]
pub struct Token {
    pub address: Address,
    pub decimals: u8,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Market {
    pub orderbook: Address,
    pub base: String,
    pub quote: String,
    pub price_precision: u64,
    pub size_precision: u64,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Kuru {
    pub router: Address,
    pub margin_account: Address,
    pub active_vault: Address,
    pub markets: BTreeMap<String, Market>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Pool {
    pub pool: Address,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Vault {
    pub asset: String,
    pub vault: Address,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Lending {
    pub aave_v3: Pool,
    pub neverland: Pool,
    pub erc4626: Vec<Vault>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Staking {
    pub shmonad: Address,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Perpl {
    pub exchange: Address,
    pub perp_ids: BTreeMap<String, u64>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct StablePool {
    pub pool: Address,
    pub coins: BTreeMap<String, i128>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Swap {
    pub ausd3pool: StablePool,
}

/// contracts/deployments/143.json: the only source of addresses.
#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Deployments {
    pub chain_id: u64,
    #[serde(default)]
    pub treasury: String,
    pub tokens: BTreeMap<String, Token>,
    pub kuru: Kuru,
    pub lending: Lending,
    pub staking: Staking,
    pub perpl: Perpl,
    pub swap: Swap,
}

pub const CHAIN_ID: u64 = 143;

impl Deployments {
    pub fn load(path: &str) -> Result<Self, String> {
        let raw = std::fs::read_to_string(path).map_err(|e| format!("{path}: {e}"))?;
        serde_json::from_str(&raw).map_err(|e| format!("{path}: {e}"))
    }

    /// "" until the user sets it; fee steps are refused meanwhile.
    pub fn treasury(&self) -> Option<Address> {
        self.treasury.parse().ok().filter(|a: &Address| !a.is_zero())
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
    pub max_step_usd: u64,
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
        if deployments.chain_id != CHAIN_ID {
            return Err(format!("deployments are for chain {}, executor signs on {CHAIN_ID} only", deployments.chain_id));
        }
        Ok(Self {
            addr: opt("EXECUTOR_ADDR").unwrap_or_else(|| "0.0.0.0:8083".into()),
            rpc_url: require("RPC_URL")?,
            database_url: require("DATABASE_URL")?,
            internal_secret: require("INTERNAL_SECRET")?,
            privy_app_id: require("PRIVY_APP_ID")?,
            privy_app_secret: require("PRIVY_APP_SECRET")?,
            privy_authorization_key: require("PRIVY_AUTHORIZATION_PRIVATE_KEY")?,
            // ponytail: one global per-step cap on stablecoin spend; per-user caps when api stores them
            max_step_usd: opt("MAX_STEP_USD").map(|v| v.parse().map_err(|_| "MAX_STEP_USD not an integer")).transpose()?.unwrap_or(1000),
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
