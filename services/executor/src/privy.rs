//! Privy wallet API. Authorization signature per
//! https://docs.privy.io/controls/authorization-keys/using-owners/sign/direct-implementation
use alloy::primitives::{hex, Address, U256};
use base64::{engine::general_purpose::STANDARD, Engine};
use p256::{
    ecdsa::{signature::Signer, DerSignature, SigningKey},
    pkcs8::DecodePrivateKey,
};
use reqwest::Method;
use serde_json::{json, Value};

use crate::{config::CHAIN_ID, gas::Fees};

pub const API: &str = "https://api.privy.io";

pub struct Authorizer(SigningKey);

impl std::fmt::Debug for Authorizer {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str("Authorizer(<redacted>)")
    }
}

impl Authorizer {
    pub fn new(key: &str) -> Result<Self, String> {
        let b64 = key.trim().strip_prefix("wallet-auth:").ok_or("authorization key must start with wallet-auth:")?;
        let der = STANDARD.decode(b64).map_err(|_| "authorization key is not base64")?;
        Ok(Self(SigningKey::from_pkcs8_der(&der).map_err(|_| "authorization key is not PKCS#8 P-256")?))
    }

    /// ECDSA P-256 / SHA-256 over the canonical payload, DER, base64.
    pub fn sign(&self, method: &str, url: &str, app_id: &str, body: &Value) -> String {
        let sig: DerSignature = self.0.sign(canonical_payload(method, url, app_id, body).as_bytes());
        STANDARD.encode(sig.as_bytes())
    }
}

// RFC-8785 holds because serde_json (no preserve_order) sorts keys and every value is a string or small int.
fn canonical_payload(method: &str, url: &str, app_id: &str, body: &Value) -> String {
    let body = match body.as_object() {
        Some(o) if o.is_empty() => Value::String(String::new()),
        _ => body.clone(),
    };
    json!({
        "body": body,
        "headers": { "privy-app-id": app_id },
        "method": method,
        "url": url.trim_end_matches('/'),
        "version": 1,
    })
    .to_string()
}

pub struct Privy {
    http: reqwest::Client,
    app_id: String,
    basic: String,
    auth: Authorizer,
}

pub fn tx_body(to: Address, value: U256, data: &[u8], fees: &Fees) -> Value {
    json!({
        "method": "eth_sendTransaction",
        "caip2": format!("eip155:{CHAIN_ID}"),
        "chain_type": "ethereum",
        "params": { "transaction": {
            "to": to.to_string(),
            "value": format!("{value:#x}"),
            "data": format!("0x{}", hex::encode(data)),
            "chain_id": CHAIN_ID,
            "type": 2,
            "gas_limit": format!("{:#x}", fees.gas_limit),
            "max_fee_per_gas": format!("{:#x}", fees.max_fee_per_gas),
            "max_priority_fee_per_gas": format!("{:#x}", fees.max_priority_fee_per_gas),
        }},
    })
}

impl Privy {
    pub fn new(app_id: String, secret: &str, auth: Authorizer) -> Self {
        Self { basic: STANDARD.encode(format!("{app_id}:{secret}")), http: reqwest::Client::new(), app_id, auth }
    }

    /// signs every non-GET: Privy does not require it on GET.
    pub async fn request(&self, method: Method, path: &str, body: Option<Value>) -> Result<Value, String> {
        let url = format!("{API}{path}");
        let mut req = self
            .http
            .request(method.clone(), &url)
            .header("Authorization", format!("Basic {}", self.basic))
            .header("privy-app-id", &self.app_id);
        if let Some(b) = &body {
            req = req.header("privy-authorization-signature", self.auth.sign(method.as_str(), &url, &self.app_id, b)).json(b);
        }
        let resp = req.send().await.map_err(|e| e.to_string())?;
        let status = resp.status();
        let text = resp.text().await.unwrap_or_default();
        if !status.is_success() {
            return Err(format!("privy {method} {path}: {status}: {text}"));
        }
        serde_json::from_str(&text).map_err(|e| format!("privy {path}: {e}"))
    }

    /// POST /v1/wallets/{id}/rpc eth_sendTransaction → data.hash.
    /// https://docs.privy.io/api-reference/wallets/ethereum/eth-send-transaction
    pub async fn send_transaction(&self, wallet_id: &str, to: Address, value: U256, data: &[u8], fees: &Fees) -> Result<String, String> {
        let v = self
            .request(Method::POST, &format!("/v1/wallets/{wallet_id}/rpc"), Some(tx_body(to, value, data, fees)))
            .await?;
        v.pointer("/data/hash").and_then(Value::as_str).map(str::to_string).ok_or_else(|| format!("privy: no data.hash in {v}"))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use p256::ecdsa::signature::Verifier;

    // throwaway key from the old executor's tests, not a real credential
    const TEST_KEY: &str = "wallet-auth:MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQg5kN2TyEXdzWAq5FQ0/xjUNzQWwR+LhLrsCbG0ywyaGuhRANCAARYefQCTLnZl3qq03dgaeiVDKh/V5ta9RHTiSq9SyzXkgVWJuFa3PRc8rLwzIeqSjMaidupsBsHWpeR3xdgBJGY";

    #[test]
    fn canonical_payload_is_sorted_and_minimal() {
        let got = canonical_payload("POST", "https://api.privy.io/v1/wallets/w1/rpc/", "app-1", &json!({"method": "m", "caip2": "c"}));
        assert_eq!(
            got,
            r#"{"body":{"caip2":"c","method":"m"},"headers":{"privy-app-id":"app-1"},"method":"POST","url":"https://api.privy.io/v1/wallets/w1/rpc","version":1}"#
        );
        assert!(canonical_payload("POST", "u", "a", &json!({})).starts_with(r#"{"body":"","#));
    }

    // byte-verified against privy's own SDK in the old executor. do not regenerate.
    #[test]
    fn matches_privy_sdk_golden_vectors() {
        let a = Authorizer::new(TEST_KEY).unwrap();
        assert_eq!(
            a.sign(
                "POST",
                "https://api.privy.io/v1/wallets/w1/rpc",
                "app-1",
                &json!({"method":"eth_sendTransaction","caip2":"eip155:8453","params":{"transaction":{"to":"0xabc","value":"0x0","chain_id":8453,"data":"0x00"}}}),
            ),
            "MEQCIGtB2l7qLSg9eRu33UWBytH+LOF5ThvYNH03Qi+hF/sXAiA3WNTFeh4X30NPPMFdfIDRaQ4DK5OslLZj3myjHnGFAg=="
        );
        assert_eq!(
            a.sign("POST", "https://api.privy.io/v1/wallets", "app-1", &json!({})),
            "MEUCIQDoBeM6a8GZfQephvYgxuA+G4uc+QB/MEpbdTo39/rxUAIgRk8wg//j9q64RoO8OtnbThE72+M1pmYF9F5AAS+wXKI="
        );
        let body = json!({"name": "p"});
        let sig = STANDARD.decode(a.sign("PATCH", "https://api.privy.io/v1/policies/x", "app-1", &body)).unwrap();
        a.0.verifying_key()
            .verify(canonical_payload("PATCH", "https://api.privy.io/v1/policies/x", "app-1", &body).as_bytes(), &DerSignature::try_from(sig.as_slice()).unwrap())
            .unwrap();
    }

    #[test]
    fn debug_does_not_leak_key() {
        assert_eq!(format!("{:?}", Authorizer::new(TEST_KEY).unwrap()), "Authorizer(<redacted>)");
    }

    #[test]
    fn tx_has_explicit_gas_value_and_monad_mainnet_caip2() {
        let f = Fees { gas_limit: 900_000, max_fee_per_gas: 201_000_000_000, max_priority_fee_per_gas: 1 };
        let b = tx_body(Address::repeat_byte(0x11), U256::from(1000), &[0xde, 0xad], &f);
        assert_eq!(b["caip2"], "eip155:143");
        assert_eq!(b["params"]["transaction"]["chain_id"], 143);
        assert_eq!(b["params"]["transaction"]["value"], "0x3e8");
        assert_eq!(b["params"]["transaction"]["gas_limit"], "0xdbba0");
        assert_eq!(b["params"]["transaction"]["data"], "0xdead");
    }
}
