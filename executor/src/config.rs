use std::{collections::HashMap, env, time::Duration};

use alloy_primitives::{address, Address};

pub const CHAIN_IDS: [u64; 2] = [143, 10143];
pub const DEFAULT_CHAIN_ID: u64 = 10143;
// AccountCore proxy: custodian, account registry and signer authority. there is
// no router in spot v2 - orders go straight to the market proxy.
pub const ACCOUNT_CORE: Address = address!("6384e9b2Bf3b65e1535403a0A543b5FDA905eE22");
// the Perpl Exchange proxy on testnet. the context must name this same address.
pub const PERPL_EXCHANGE: Address = address!("1964c32f0be608e7d29302aff5e61268e72080cc");

/// the backend key holding TRADE on the user's account. unset means the user's
/// own privy wallet signs for its own account.
#[derive(Clone, Debug)]
pub struct Signer {
    pub wallet_id: String,
    pub address: Address,
}

pub struct Config {
    pub addr: String,
    pub privy_app_id: String,
    pub privy_app_secret: String,
    pub privy_authorization_key: Option<String>,
    pub markets_path: String,
    pub account_core: Address,
    pub signer: Option<Signer>,
    pub rpc_urls: HashMap<u64, String>,
    pub receipt_timeout: Duration,
    pub perpl_api_url: String,
    pub perpl_exchange: Address,
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
            account_core: match opt("KURU_ACCOUNT_CORE") {
                Some(v) => v.parse().map_err(|_| format!("KURU_ACCOUNT_CORE is not an address: {v}"))?,
                None => ACCOUNT_CORE,
            },
            signer: match (opt("KURU_SIGNER_WALLET_ID"), opt("KURU_SIGNER_ADDRESS")) {
                (Some(wallet_id), Some(a)) => Some(Signer {
                    wallet_id,
                    address: a.parse().map_err(|_| format!("KURU_SIGNER_ADDRESS is not an address: {a}"))?,
                }),
                (None, None) => None,
                _ => return Err("KURU_SIGNER_WALLET_ID and KURU_SIGNER_ADDRESS must be set together".into()),
            },
            rpc_urls: CHAIN_IDS.iter().map(|&id| (id, rpc_url(id))).collect(),
            receipt_timeout: Duration::from_secs(
                opt("RECEIPT_TIMEOUT").and_then(|v| v.parse().ok()).unwrap_or(60),
            ),
            perpl_api_url: opt("PERPL_API_URL").unwrap_or_else(|| "https://testnet.perpl.xyz/api".into()),
            perpl_exchange: match opt("PERPL_EXCHANGE_ADDRESS") {
                Some(v) => v.parse().map_err(|_| format!("PERPL_EXCHANGE_ADDRESS is not an address: {v}"))?,
                None => PERPL_EXCHANGE,
            },
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
