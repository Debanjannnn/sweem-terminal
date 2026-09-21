use std::{collections::HashMap, env, time::Duration};

use alloy_primitives::{address, Address};

pub const CHAIN_IDS: [u64; 2] = [143, 10143];
// kuru router proxy. verified live on testnet.
pub const KURU_ROUTER: Address = address!("7EFbE105Ca7415dE98F96622173458ac1c054630");

pub struct Config {
    pub addr: String,
    pub privy_app_id: String,
    pub privy_app_secret: String,
    pub privy_authorization_key: Option<String>,
    pub markets_path: String,
    pub kuru_router: Address,
    pub rpc_urls: HashMap<u64, String>,
    pub receipt_timeout: Duration,
}

impl Config {
    pub fn from_env() -> Result<Self, String> {
        load_dotenv(".env");
        load_dotenv("../.env");
        Ok(Self {
            addr: env::var("EXECUTOR_ADDR").unwrap_or_else(|_| {
                format!(
                    "0.0.0.0:{}",
                    env::var("EXECUTOR_PORT").unwrap_or_else(|_| "8083".into())
                )
            }),
            privy_app_id: require("PRIVY_APP_ID")?,
            privy_app_secret: require("PRIVY_APP_SECRET")?,
            privy_authorization_key: opt("PRIVY_AUTHORIZATION_PRIVATE_KEY"),
            markets_path: env::var("KURU_MARKETS_PATH").unwrap_or_else(|_| "kuru-markets.json".into()),
            kuru_router: match opt("KURU_ROUTER") {
                Some(v) => v.parse().map_err(|_| format!("KURU_ROUTER is not an address: {v}"))?,
                None => KURU_ROUTER,
            },
            rpc_urls: CHAIN_IDS.iter().map(|&id| (id, rpc_url(id))).collect(),
            receipt_timeout: Duration::from_secs(
                opt("RECEIPT_TIMEOUT").and_then(|v| v.parse().ok()).unwrap_or(60),
            ),
        })
    }
}

fn rpc_url(chain_id: u64) -> String {
    let default = match chain_id {
        143 => "https://rpc.monad.xyz",
        _ => "https://testnet-rpc.monad.xyz",
    };
    opt(&format!("MONAD_RPC_URL_{chain_id}")).unwrap_or_else(|| default.into())
}

fn opt(key: &str) -> Option<String> {
    env::var(key).ok().map(|v| v.trim().to_string()).filter(|v| !v.is_empty())
}

fn require(key: &str) -> Result<String, String> {
    opt(key).ok_or_else(|| format!("missing required env var {key}"))
}

fn load_dotenv(path: &str) {
    let Ok(body) = std::fs::read_to_string(path) else {
        return;
    };
    for line in body.lines() {
        let line = line.trim();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        if let Some((k, v)) = line.split_once('=') {
            if env::var(k.trim()).is_err() {
                unsafe { env::set_var(k.trim(), v.trim()) };
            }
        }
    }
}
