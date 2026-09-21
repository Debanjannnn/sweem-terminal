use base64::{engine::general_purpose::STANDARD, Engine};
use serde::Deserialize;
use serde_json::{json, Value};

use crate::{auth::Authorizer, gas::Fees, venues::Call};

const PRIVY_API: &str = "https://api.privy.io";

#[derive(Debug, thiserror::Error)]
pub enum PrivyError {
    #[error("privy request failed: {0}")]
    Http(#[from] reqwest::Error),
    #[error("privy returned {status}: {body}")]
    Status { status: u16, body: String },
    #[error("privy response was not the expected shape: {0}")]
    Shape(String),
}

pub struct Privy {
    http: reqwest::Client,
    app_id: String,
    basic_auth: String,
    authorizer: Option<Authorizer>,
}

#[derive(Debug, Deserialize)]
pub struct TxData {
    pub hash: String,
    #[serde(default)]
    pub transaction_id: String,
}

#[derive(Debug, Deserialize)]
pub struct Wallet {
    pub address: String,
    #[serde(default)]
    pub chain_type: String,
}

fn tx_body(chain_id: u64, call: &Call, fees: &Fees, reference_id: &str) -> Value {
    json!({
        "method": "eth_sendTransaction",
        "caip2": format!("eip155:{chain_id}"),
        "chain_type": "ethereum",
        "params": { "transaction": {
            "to": call.to.to_string(),
            "value": format!("{:#x}", call.value),
            "data": format!("0x{}", alloy_primitives::hex::encode(&call.data)),
            "chain_id": chain_id,
            "type": 2,
            "gas_limit": format!("{:#x}", fees.gas_limit),
            "max_fee_per_gas": format!("{:#x}", fees.max_fee_per_gas),
            "max_priority_fee_per_gas": format!("{:#x}", fees.max_priority_fee_per_gas),
        }},
        // not an idempotency key: privy 400s on a reused one
        "reference_id": reference_id,
    })
}

impl Privy {
    pub fn new(app_id: String, app_secret: String, authorizer: Option<Authorizer>) -> Self {
        Self {
            basic_auth: STANDARD.encode(format!("{app_id}:{app_secret}")),
            http: reqwest::Client::new(),
            app_id,
            authorizer,
        }
    }

    fn signature_header(&self, url: &str, body: &Value) -> Option<String> {
        Some(self.authorizer.as_ref()?.sign(url, &self.app_id, body))
    }

    async fn post_rpc(&self, wallet_id: &str, body: Value, idempotency_key: &str) -> Result<Value, PrivyError> {
        let url = format!("{PRIVY_API}/v1/wallets/{wallet_id}/rpc");
        let mut req = self
            .http
            .post(&url)
            .header("Authorization", format!("Basic {}", self.basic_auth))
            .header("privy-app-id", &self.app_id)
            .header("privy-idempotency-key", idempotency_key)
            .header("Content-Type", "application/json");
        if let Some(sig) = self.signature_header(&url, &body) {
            req = req.header("privy-authorization-signature", sig);
        }
        let resp = req.json(&body).send().await?;
        let status = resp.status();
        if !status.is_success() {
            return Err(PrivyError::Status {
                status: status.as_u16(),
                body: resp.text().await.unwrap_or_default(),
            });
        }
        Ok(resp.json().await?)
    }

    /// `gas_limit` is always explicit: monad debits the whole limit at submission.
    pub async fn send_transaction(
        &self,
        wallet_id: &str,
        chain_id: u64,
        call: &Call,
        fees: &Fees,
        idempotency_key: &str,
        reference_id: &str,
    ) -> Result<TxData, PrivyError> {
        let v = self
            .post_rpc(wallet_id, tx_body(chain_id, call, fees, reference_id), idempotency_key)
            .await?;
        serde_json::from_value(v.get("data").cloned().unwrap_or(v))
            .map_err(|e| PrivyError::Shape(e.to_string()))
    }

    // nothing on the kuru market-order path signs offchain yet
    #[allow(dead_code)]
    pub async fn sign_typed_data(
        &self,
        wallet_id: &str,
        typed_data: Value,
        idempotency_key: &str,
    ) -> Result<String, PrivyError> {
        let body = json!({
            "method": "eth_signTypedData_v4",
            "chain_type": "ethereum",
            "params": { "typed_data": typed_data },
        });
        let v = self.post_rpc(wallet_id, body, idempotency_key).await?;
        v.pointer("/data/signature")
            .and_then(|s| s.as_str())
            .map(str::to_string)
            .ok_or_else(|| PrivyError::Shape(v.to_string()))
    }

    pub async fn get_wallet(&self, wallet_id: &str) -> Result<Wallet, PrivyError> {
        let resp = self
            .http
            .get(format!("{PRIVY_API}/v1/wallets/{wallet_id}"))
            .header("Authorization", format!("Basic {}", self.basic_auth))
            .header("privy-app-id", &self.app_id)
            .send()
            .await?;
        let status = resp.status();
        if !status.is_success() {
            return Err(PrivyError::Status {
                status: status.as_u16(),
                body: resp.text().await.unwrap_or_default(),
            });
        }
        Ok(resp.json().await?)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn privy(signed: bool) -> Privy {
        let a = signed.then(|| Authorizer::new(crate::auth::TEST_KEY).unwrap());
        Privy::new("app".into(), "secret".into(), a)
    }

    #[test]
    fn header_present_only_when_a_key_is_configured() {
        let url = "https://api.privy.io/v1/wallets/w1/rpc";
        let body = json!({"method": "eth_sendTransaction"});
        assert!(privy(true).signature_header(url, &body).is_some());
        assert!(privy(false).signature_header(url, &body).is_none());
    }

    #[test]
    fn every_transaction_carries_an_explicit_gas_limit() {
        let call = Call {
            to: alloy_primitives::Address::repeat_byte(0x11),
            value: alloy_primitives::U256::from(1_000u64),
            data: vec![0xde, 0xad].into(),
        };
        let fees = Fees { gas_limit: 900_000, max_fee_per_gas: 201_000_000_000, max_priority_fee_per_gas: 1_000_000_000 };
        let b = tx_body(10143, &call, &fees, "ref-1");
        let tx = &b["params"]["transaction"];
        assert_eq!(tx["gas_limit"], "0xdbba0", "an unset limit is billed at privy's default and fails");
        assert_eq!(tx["max_fee_per_gas"], "0x2ecc889a00");
        assert_eq!(tx["value"], "0x3e8");
        assert_eq!(tx["type"], 2);
        assert_eq!(tx["chain_id"], 10143);
        assert_eq!(b["caip2"], "eip155:10143");
        assert_eq!(b["method"], "eth_sendTransaction");
        assert_eq!(b["reference_id"], "ref-1");
    }

    #[test]
    fn tx_response_unwraps_the_data_envelope() {
        let d: TxData = serde_json::from_value(json!({"hash":"0xab","transaction_id":"t1"})).unwrap();
        assert_eq!(d.hash, "0xab");
        let d: TxData = serde_json::from_value(json!({"hash":"0xab"})).unwrap();
        assert_eq!(d.transaction_id, "");
    }
}
