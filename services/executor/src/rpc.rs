use alloy::primitives::{hex, Address, Bytes, B256, U256};
use serde::Deserialize;
use serde_json::{json, Value};
use std::time::Duration;

pub struct Rpc {
    http: reqwest::Client,
    url: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Log {
    pub address: Address,
    pub topics: Vec<B256>,
    pub data: Bytes,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Receipt {
    pub status: U256,
    pub block_number: U256,
    #[serde(default)]
    pub logs: Vec<Log>,
}

impl Receipt {
    pub fn ok(&self) -> bool {
        self.status == U256::from(1)
    }
}

fn tx(from: Address, to: Address, value: U256, data: &[u8]) -> Value {
    json!({"from": from, "to": to, "value": format!("{value:#x}"), "data": format!("0x{}", hex::encode(data))})
}

fn quantity(v: &Value) -> Result<u128, String> {
    let s = v.as_str().ok_or_else(|| format!("not a quantity: {v}"))?;
    u128::from_str_radix(s.trim_start_matches("0x"), 16).map_err(|e| e.to_string())
}

impl Rpc {
    pub fn new(url: String) -> Self {
        let http = reqwest::Client::builder().timeout(Duration::from_secs(15)).build().expect("reqwest client");
        Self { http, url }
    }

    /// Err carries the node's error object: the revert selector lives there.
    pub async fn call(&self, method: &str, params: Value) -> Result<Value, String> {
        let body = json!({"jsonrpc": "2.0", "id": 1, "method": method, "params": params});
        let v: Value = self.http.post(&self.url).json(&body).send().await.map_err(|e| e.to_string())?
            .json().await.map_err(|e| e.to_string())?;
        if let Some(e) = v.get("error") {
            return Err(format!("{method}: {e}"));
        }
        Ok(v.get("result").cloned().unwrap_or(Value::Null))
    }

    pub async fn eth_call(&self, to: Address, data: &[u8]) -> Result<Vec<u8>, String> {
        let out = self.call("eth_call", json!([tx(Address::ZERO, to, U256::ZERO, data), "latest"])).await?;
        hex::decode(out.as_str().unwrap_or_default()).map_err(|e| e.to_string())
    }

    pub async fn estimate_gas(&self, from: Address, to: Address, value: U256, data: &[u8]) -> Result<u64, String> {
        let v = self.call("eth_estimateGas", json!([tx(from, to, value, data), "latest"])).await?;
        Ok(quantity(&v)? as u64)
    }

    pub async fn base_fee(&self) -> Result<u128, String> {
        let b = self.call("eth_getBlockByNumber", json!(["latest", false])).await?;
        quantity(&b["baseFeePerGas"])
    }

    pub async fn priority_fee(&self) -> Result<u128, String> {
        quantity(&self.call("eth_maxPriorityFeePerGas", json!([])).await?)
    }

    pub async fn block_number(&self) -> Result<u64, String> {
        Ok(quantity(&self.call("eth_blockNumber", json!([])).await?)? as u64)
    }

    pub async fn pending_nonce(&self, who: Address) -> Result<u64, String> {
        Ok(quantity(&self.call("eth_getTransactionCount", json!([who, "pending"])).await?)? as u64)
    }

    pub async fn send_raw(&self, raw: &[u8]) -> Result<B256, String> {
        let v = self.call("eth_sendRawTransaction", json!([format!("0x{}", hex::encode(raw))])).await?;
        serde_json::from_value(v).map_err(|e| e.to_string())
    }

    /// None while mempool-only: monad returns null until the tx is proposed.
    pub async fn receipt(&self, hash: &str) -> Result<Option<Receipt>, String> {
        let v = self.call("eth_getTransactionReceipt", json!([hash])).await?;
        if v.is_null() {
            return Ok(None);
        }
        serde_json::from_value(v).map(Some).map_err(|e| e.to_string())
    }

    pub async fn wait_receipt(&self, hash: &str, timeout: Duration) -> Option<Receipt> {
        let deadline = tokio::time::Instant::now() + timeout;
        while tokio::time::Instant::now() < deadline {
            if let Ok(Some(r)) = self.receipt(hash).await {
                return Some(r);
            }
            tokio::time::sleep(Duration::from_millis(500)).await;
        }
        None
    }
}
