//! `executor sync-policy`: the Privy policy on user wallets is a backstop allowlist:
//! chain 10143, USDC.approve(spender ∈ baskets), basket.mint/redeem. Anything else hits Privy's default deny.
//! https://docs.privy.io/api-reference/policies/update · /create · https://docs.privy.io/controls/policies/overview
use alloy::{primitives::Address, sol_types::SolCall};
use reqwest::Method;
use serde_json::{json, Value};

use crate::{config::Deployments, contracts::basketsCall, privy::Privy, rpc::Rpc};

fn abi(name: &str, inputs: &[(&str, &str)]) -> Value {
    let inputs: Vec<Value> = inputs.iter().map(|(n, t)| json!({"name": n, "type": t})).collect();
    json!([{"name": name, "type": "function", "inputs": inputs, "outputs": [], "stateMutability": "nonpayable"}])
}

fn cond(source: &str, field: &str, op: &str, value: Value) -> Value {
    json!({"field_source": source, "field": field, "operator": op, "value": value})
}

fn call_cond(abi: &Value, field: &str, op: &str, value: Value) -> Value {
    let mut c = cond("ethereum_calldata", field, op, value);
    c["abi"] = abi.clone();
    c
}

pub fn rules(usdc: Address, baskets: &[Address]) -> Vec<Value> {
    let lower = |a: &Address| a.to_string().to_lowercase();
    let baskets: Vec<String> = baskets.iter().map(lower).collect();
    let chain = cond("ethereum_transaction", "chain_id", "eq", json!("10143"));
    let to_baskets = cond("ethereum_transaction", "to", "in", json!(baskets));
    let approve = abi("approve", &[("spender", "address"), ("amount", "uint256")]);
    let mint = abi("mint", &[("usdcIn", "uint256"), ("minShares", "uint256"), ("deadline", "uint256")]);
    let redeem = abi("redeem", &[("shares", "uint256"), ("minUsdcOut", "uint256"), ("deadline", "uint256")]);
    let rule = |name: &str, conditions: Vec<Value>| json!({"name": name, "method": "eth_sendTransaction", "action": "ALLOW", "conditions": conditions});
    vec![
        rule("USDC approve to sweem baskets", vec![
            chain.clone(),
            cond("ethereum_transaction", "to", "eq", json!(lower(&usdc))),
            call_cond(&approve, "function_name", "eq", json!("approve")),
            call_cond(&approve, "approve.spender", "in", json!(baskets)),
        ]),
        rule("Sweem basket mint", vec![chain.clone(), to_baskets.clone(), call_cond(&mint, "function_name", "eq", json!("mint"))]),
        rule("Sweem basket redeem", vec![chain, to_baskets, call_cond(&redeem, "function_name", "eq", json!("redeem"))]),
    ]
}

const NAME: &str = "sweem baskets executor";

pub async fn baskets(rpc: &Rpc, dep: &Deployments) -> Result<Vec<Address>, String> {
    let out = rpc.eth_call(dep.factory, &basketsCall {}.abi_encode()).await?;
    let mut b = basketsCall::abi_decode_returns(&out).map_err(|e| e.to_string())?;
    b.sort();
    Ok(b)
}

/// policy only needs rewriting when the sorted basket set moved.
pub fn changed(last: Option<&[Address]>, now: &[Address]) -> bool {
    last != Some(now)
}

/// startup sync, then every 60s only when factory baskets() differs. a failed sync retries next tick.
pub async fn watch(privy: std::sync::Arc<Privy>, rpc: std::sync::Arc<Rpc>, dep: Deployments, policy_id: Option<String>, quorum: Option<String>) {
    let mut last: Option<Vec<Address>> = None;
    loop {
        match baskets(&rpc, &dep).await {
            Ok(now) if changed(last.as_deref(), &now) => match apply(&privy, &dep, &now, policy_id.clone(), quorum.clone()).await {
                Ok(p) => {
                    tracing::info!(policy = %p["id"], baskets = now.len(), "privy policy synced");
                    last = Some(now);
                }
                Err(e) => tracing::error!(error = %e, "privy policy sync"),
            },
            Ok(_) => {}
            Err(e) => tracing::warn!(error = %e, "read factory baskets"),
        }
        tokio::time::sleep(std::time::Duration::from_secs(60)).await;
    }
}

pub async fn apply(privy: &Privy, dep: &Deployments, baskets: &[Address], policy_id: Option<String>, quorum: Option<String>) -> Result<Value, String> {
    if baskets.is_empty() {
        return Err("factory has no baskets: refusing a policy that allows nothing but approve".into());
    }
    let rules = rules(dep.kuru.usdc, baskets);
    let existing = match &policy_id {
        Some(id) => match privy.request(Method::GET, &format!("/v1/policies/{id}"), None).await {
            Ok(_) => Some(id.clone()),
            Err(e) if e.contains("404") => None,
            Err(e) => return Err(e),
        },
        None => None,
    };
    match existing {
        Some(id) => privy.request(Method::PATCH, &format!("/v1/policies/{id}"), Some(json!({"name": NAME, "rules": rules}))).await,
        None => {
            let mut body = json!({"version": "1.0", "name": NAME, "chain_type": "ethereum", "rules": rules});
            if let Some(q) = quorum {
                body["owner_id"] = json!(q);
            }
            privy.request(Method::POST, "/v1/policies", Some(body)).await
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn rules_pin_chain_targets_and_functions() {
        let usdc: Address = "0xEe0722ead54f1B4fe97bE399Be43BC0226a6f97E".parse().unwrap();
        let b: Address = "0xE0b3bAafa8EDD1db95b49e62ea0fB18c04353870".parse().unwrap();
        let r = rules(usdc, &[b]);
        assert_eq!(r.len(), 3);
        for rule in &r {
            assert_eq!(rule["method"], "eth_sendTransaction");
            assert_eq!(rule["action"], "ALLOW");
            assert!(rule["name"].as_str().unwrap().len() <= 50);
            assert_eq!(rule["conditions"][0], json!({"field_source":"ethereum_transaction","field":"chain_id","operator":"eq","value":"10143"}));
        }
        assert_eq!(r[0]["conditions"][1]["value"], "0xee0722ead54f1b4fe97be399be43bc0226a6f97e");
        assert_eq!(r[0]["conditions"][3]["value"], json!(["0xe0b3baafa8edd1db95b49e62ea0fb18c04353870"]));
        assert_eq!(r[1]["conditions"][2]["value"], "mint");
        assert_eq!(r[2]["conditions"][2]["value"], "redeem");
    }

    #[test]
    fn diff_detection() {
        let (a, b) = (Address::repeat_byte(1), Address::repeat_byte(2));
        assert!(changed(None, &[a]), "first tick always syncs");
        assert!(!changed(Some(&[a, b]), &[a, b]));
        assert!(changed(Some(&[a]), &[a, b]), "new basket");
        assert!(changed(Some(&[a, b]), &[a]), "removed basket");
    }
}
