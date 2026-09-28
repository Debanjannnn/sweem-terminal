use alloy_primitives::{hex, Address, U256};
use serde_json::json;
use std::{future::Future, time::Duration};

pub const POLL_INTERVAL: Duration = Duration::from_secs(1);

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Outcome {
    Confirmed,
    Reverted,
}

pub struct Rpc {
    http: reqwest::Client,
    url: String,
}

impl Rpc {
    pub fn new(url: String) -> Self {
        Self { http: reqwest::Client::new(), url }
    }

    async fn try_call(&self, method: &str, params: serde_json::Value) -> Result<serde_json::Value, String> {
        let body = json!({"jsonrpc": "2.0", "id": 1, "method": method, "params": params});
        let resp = self.http.post(&self.url).json(&body).send().await.map_err(|e| e.to_string())?;
        let v: serde_json::Value = resp.json().await.map_err(|e| e.to_string())?;
        if let Some(e) = v.get("error") {
            // carries the revert selector, which is the only clue a custom error gives
            return Err(e.to_string());
        }
        v.get("result").cloned().filter(|r| !r.is_null()).ok_or_else(|| "null result".into())
    }

    async fn call(&self, method: &str, params: serde_json::Value) -> Option<serde_json::Value> {
        self.try_call(method, params)
            .await
            .inspect_err(|e| tracing::debug!(%method, error = %e, "rpc error"))
            .ok()
    }

    fn quantity(v: &serde_json::Value) -> Option<u128> {
        u128::from_str_radix(v.as_str()?.trim_start_matches("0x"), 16).ok()
    }

    /// `latest` on monad is the proposed (speculative) block.
    pub async fn eth_call(&self, from: Address, to: Address, value: U256, data: &[u8]) -> Result<Vec<u8>, String> {
        let tx = json!({
            "from": from.to_string(),
            "to": to.to_string(),
            "value": format!("{value:#x}"),
            "data": format!("0x{}", hex::encode(data)),
        });
        let out = self.try_call("eth_call", json!([tx, "latest"])).await?;
        hex::decode(out.as_str().unwrap_or_default()).map_err(|e| e.to_string())
    }

    pub async fn estimate_gas(&self, from: Address, to: Address, value: U256, data: &[u8]) -> Option<u64> {
        let tx = json!({
            "from": from.to_string(),
            "to": to.to_string(),
            "value": format!("{value:#x}"),
            "data": format!("0x{}", hex::encode(data)),
        });
        Self::quantity(&self.call("eth_estimateGas", json!([tx, "latest"])).await?)?.try_into().ok()
    }

    pub async fn balance(&self, who: Address) -> Option<U256> {
        let v = self.call("eth_getBalance", json!([who.to_string(), "latest"])).await?;
        U256::from_str_radix(v.as_str()?.trim_start_matches("0x"), 16).ok()
    }

    pub async fn base_fee(&self) -> Option<u128> {
        let b = self.call("eth_getBlockByNumber", json!(["latest", false])).await?;
        Self::quantity(b.get("baseFeePerGas")?)
    }

    pub async fn priority_fee(&self) -> Option<u128> {
        Self::quantity(&self.call("eth_maxPriorityFeePerGas", json!([])).await?)
    }

    pub async fn block_number(&self) -> Option<u64> {
        Self::quantity(&self.call("eth_blockNumber", json!([])).await?)?.try_into().ok()
    }

    /// `None` while the tx is still mempool-only: monad returns null for both
    /// the receipt and eth_getTransactionByHash until it is proposed.
    pub async fn receipt(&self, hash: &str) -> Option<Outcome> {
        let r = self.call("eth_getTransactionReceipt", json!([hash])).await?;
        Some(match r.get("status")?.as_str()? {
            "0x1" => Outcome::Confirmed,
            _ => Outcome::Reverted,
        })
    }
}

/// the receipt's own logs, for venues whose outcome is only in events.
pub type ReceiptLogs = (Outcome, Vec<crate::venues::perpl::Log>);

impl Rpc {
    pub async fn receipt_logs(&self, hash: &str) -> Option<ReceiptLogs> {
        let r = self.call("eth_getTransactionReceipt", json!([hash])).await?;
        let outcome = match r.get("status")?.as_str()? {
            "0x1" => Outcome::Confirmed,
            _ => Outcome::Reverted,
        };
        let logs = r
            .get("logs")?
            .as_array()?
            .iter()
            .filter_map(|l| {
                Some(crate::venues::perpl::Log {
                    address: l.get("address")?.as_str()?.parse().ok()?,
                    topics: l.get("topics")?.as_array()?.iter().filter_map(|t| t.as_str()?.parse().ok()).collect(),
                    data: hex::decode(l.get("data")?.as_str()?).ok()?,
                })
            })
            .collect();
        Some((outcome, logs))
    }
}

pub async fn wait<T, F, Fut>(mut fetch: F, timeout: Duration, interval: Duration) -> Option<T>
where
    F: FnMut() -> Fut,
    Fut: Future<Output = Option<T>>,
{
    let deadline = tokio::time::Instant::now() + timeout;
    loop {
        if let Some(o) = fetch().await {
            return Some(o);
        }
        if tokio::time::Instant::now() + interval >= deadline {
            return None;
        }
        tokio::time::sleep(interval).await;
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::cell::Cell;

    const FAST: Duration = Duration::from_millis(1);

    #[tokio::test]
    async fn returns_the_outcome_once_mined() {
        let calls = Cell::new(0);
        let got = wait(
            || {
                calls.set(calls.get() + 1);
                let n = calls.get();
                async move { (n >= 3).then_some(Outcome::Confirmed) }
            },
            Duration::from_millis(200),
            FAST,
        )
        .await;
        assert_eq!(got, Some(Outcome::Confirmed));
        assert_eq!(calls.get(), 3);
    }

    #[tokio::test]
    async fn timeout_is_none_not_failure() {
        let got: Option<Outcome> = wait(|| async { None }, Duration::from_millis(20), FAST).await;
        assert_eq!(got, None, "timeout must be distinguishable from a revert");
    }

    #[test]
    fn quantity_parses_hex() {
        assert_eq!(Rpc::quantity(&json!("0x5208")), Some(21000));
        assert_eq!(Rpc::quantity(&json!(null)), None);
    }
}
