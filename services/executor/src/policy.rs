//! `executor sync-policy [--dry-run]`: the Privy policy on user wallets is the backstop allowlist (design §4):
//! chain 143, every venue contract + method the adapters use, approve only to venue spenders, USDC transfer only to treasury.
//! Anything else hits Privy's default deny. https://docs.privy.io/controls/policies/overview
use alloy::primitives::Address;
use reqwest::Method;
use serde_json::{json, Value};

use crate::{config::{Deployments, CHAIN_ID}, privy::Privy};

fn abi_item(name: &str, inputs: Value) -> Value {
    json!([{"name": name, "type": "function", "inputs": inputs, "outputs": [], "stateMutability": "nonpayable"}])
}

fn abi(name: &str, inputs: &[(&str, &str)]) -> Value {
    abi_item(name, inputs.iter().map(|(n, t)| json!({"name": n, "type": t})).collect())
}

fn cond(source: &str, field: &str, op: &str, value: Value) -> Value {
    json!({"field_source": source, "field": field, "operator": op, "value": value})
}

fn call_cond(abi: &Value, field: &str, op: &str, value: Value) -> Value {
    let mut c = cond("ethereum_calldata", field, op, value);
    c["abi"] = abi.clone();
    c
}

fn lower(a: &Address) -> String {
    a.to_string().to_lowercase()
}

pub const TREASURY_PLACEHOLDER: &str = "<treasury: unset in 143.json>";

pub fn rules(d: &Deployments) -> Vec<Value> {
    let chain = cond("ethereum_transaction", "chain_id", "eq", json!(CHAIN_ID.to_string()));
    let rule = |name: &str, to: Vec<Address>, abi: Value, extra: Vec<Value>| {
        let f = abi[0]["name"].as_str().unwrap().to_string();
        let mut to: Vec<String> = to.iter().map(lower).collect();
        to.sort();
        to.dedup();
        let mut c = vec![chain.clone(), cond("ethereum_transaction", "to", "in", json!(to)), call_cond(&abi, "function_name", "eq", json!(f))];
        c.extend(extra);
        json!({"name": name, "method": "eth_sendTransaction", "action": "ALLOW", "conditions": c})
    };
    let obs: Vec<Address> = d.kuru.markets.values().map(|m| m.orderbook).collect();
    let (ma, router, av) = (d.kuru.margin_account, d.kuru.router, d.kuru.active_vault);
    let pools = vec![d.lending.aave_v3.pool, d.lending.neverland.pool];
    let vaults: Vec<Address> = d.lending.erc4626.iter().map(|v| v.vault).collect();
    let (stable, perpl) = (d.swap.ausd3pool.pool, d.perpl.exchange);
    let erc20s: Vec<Address> = d.tokens.values().map(|t| t.address).filter(|a| !a.is_zero()).collect();
    let (usdc, wmon) = (d.tokens["USDC"].address, d.tokens["WMON"].address);

    let mut spenders: Vec<String> = [obs.clone(), vec![ma, router, av, stable, perpl], pools.clone(), vaults.clone()].concat().iter().map(lower).collect();
    spenders.sort();
    spenders.dedup();
    let approve = abi("approve", &[("spender", "address"), ("amount", "uint256")]);
    let transfer = abi("transfer", &[("to", "address"), ("amount", "uint256")]);
    let treasury = d.treasury().map(|a| lower(&a)).unwrap_or_else(|| TREASURY_PLACEHOLDER.into());
    let shmon_vaults = [vaults.clone(), vec![d.staking.shmonad]].concat();
    let u = |n: &'static str| (n, "uint256");
    let components: Vec<Value> = [
        "orderDescId:uint256", "perpId:uint256", "orderType:uint8", "orderId:uint256", "pricePNS:uint256", "lotLNS:uint256",
        "expiryBlock:uint256", "postOnly:bool", "fillOrKill:bool", "immediateOrCancel:bool", "maxMatches:uint256",
        "leverageHdths:uint256", "lastExecutionBlock:uint256", "amountCNS:uint256", "maxNegPnlCollatBPS:uint256",
    ].iter().map(|s| { let (n, t) = s.split_once(':').unwrap(); json!({"name": n, "type": t}) }).collect();
    let order_desc = json!([{"name": "orderDesc", "type": "tuple", "components": components}]);

    vec![
        rule("ERC20 approve to venue spenders", erc20s, approve.clone(), vec![call_cond(&approve, "approve.spender", "in", json!(spenders))]),
        rule("USDC transfer to treasury only", vec![usdc], transfer.clone(), vec![call_cond(&transfer, "transfer.to", "eq", json!(treasury))]),
        rule("Kuru market buy", obs.clone(), abi("placeAndExecuteMarketBuy", &[("quoteSize", "uint96"), u("minAmountOut"), ("isMargin", "bool"), ("isFillOrKill", "bool")]), vec![]),
        rule("Kuru market sell", obs.clone(), abi("placeAndExecuteMarketSell", &[("size", "uint96"), u("minAmountOut"), ("isMargin", "bool"), ("isFillOrKill", "bool")]), vec![]),
        rule("Kuru limit buy", obs.clone(), abi("addBuyOrder", &[("price", "uint32"), ("size", "uint96"), ("postOnly", "bool")]), vec![]),
        rule("Kuru limit sell", obs.clone(), abi("addSellOrder", &[("price", "uint32"), ("size", "uint96"), ("postOnly", "bool")]), vec![]),
        rule("Kuru cancel orders", obs, abi("batchCancelOrders", &[("orderIds", "uint40[]")]), vec![]),
        rule("Kuru margin deposit", vec![ma], abi("deposit", &[("user", "address"), ("token", "address"), u("amount")]), vec![]),
        rule("Kuru margin withdraw", vec![ma], abi("withdraw", &[u("amount"), ("token", "address")]), vec![]),
        rule("Kuru router swap", vec![router], abi("anyToAnySwap", &[("markets", "address[]"), ("isBuy", "bool[]"), ("nativeSend", "bool[]"), ("debitToken", "address"), ("creditToken", "address"), u("amount"), u("minAmountOut")]), vec![]),
        rule("Aave/Neverland supply", pools.clone(), abi("supply", &[("asset", "address"), u("amount"), ("onBehalfOf", "address"), ("referralCode", "uint16")]), vec![]),
        rule("Aave/Neverland withdraw", pools, abi("withdraw", &[("asset", "address"), u("amount"), ("to", "address")]), vec![]),
        rule("ERC4626 + shMON deposit", shmon_vaults.clone(), abi("deposit", &[u("assets"), ("receiver", "address")]), vec![]),
        rule("ERC4626 + shMON withdraw", shmon_vaults.clone(), abi("withdraw", &[u("assets"), ("receiver", "address"), ("owner", "address")]), vec![]),
        rule("ERC4626 + shMON redeem", shmon_vaults, abi("redeem", &[u("shares"), ("receiver", "address"), ("owner", "address")]), vec![]),
        rule("Kuru ActiveVault deposit", vec![av], abi("deposit", &[u("baseAmount"), u("quoteAmount")]), vec![]),
        rule("Kuru ActiveVault withdraw", vec![av], abi("withdraw", &[u("shares")]), vec![]),
        rule("WMON wrap", vec![wmon], abi("deposit", &[]), vec![]),
        rule("WMON unwrap", vec![wmon], abi("withdraw", &[u("wad")]), vec![]),
        rule("AUSD 3pool exchange", vec![stable], abi("exchange", &[("i", "int128"), ("j", "int128"), u("dx"), u("min_dy")]), vec![]),
        rule("Perpl createAccount", vec![perpl], abi("createAccount", &[u("amountCNS")]), vec![]),
        rule("Perpl depositCollateral", vec![perpl], abi("depositCollateral", &[u("amountCNS")]), vec![]),
        rule("Perpl execOrder", vec![perpl], abi_item("execOrder", order_desc), vec![]),
        rule("Perpl request margin decrease", vec![perpl], abi("requestDecreasePositionCollateral", &[u("perpId"), u("amountCNS"), ("clampToMaximum", "bool")]), vec![]),
        rule("Perpl withdrawCollateral", vec![perpl], abi("withdrawCollateral", &[u("amountCNS")]), vec![]),
    ]
}

const NAME: &str = "sweem executor v2 (monad 143)";

pub async fn apply(privy: &Privy, dep: &Deployments, policy_id: Option<String>, quorum: Option<String>) -> Result<Value, String> {
    if dep.treasury().is_none() {
        return Err("treasury unset in deployments: refusing to push a policy with a placeholder".into());
    }
    let rules = rules(dep);
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

    fn dep() -> Deployments {
        Deployments::load(concat!(env!("CARGO_MANIFEST_DIR"), "/../../contracts/deployments/143.json")).unwrap()
    }

    #[test]
    fn every_rule_pins_chain_targets_and_function() {
        let mut d = dep();
        d.treasury = String::new();
        let r = rules(&d);
        assert_eq!(r.len(), 25);
        for rule in &r {
            assert_eq!(rule["method"], "eth_sendTransaction");
            assert_eq!(rule["action"], "ALLOW");
            assert!(rule["name"].as_str().unwrap().len() <= 50);
            let c = rule["conditions"].as_array().unwrap();
            assert_eq!(c[0], json!({"field_source":"ethereum_transaction","field":"chain_id","operator":"eq","value":"143"}));
            assert_eq!((c[1]["field"].as_str(), c[1]["operator"].as_str()), (Some("to"), Some("in")));
            assert!(!c[1]["value"].as_array().unwrap().is_empty());
            assert_eq!(c[2]["field"], "function_name");
        }
        let spenders = r[0]["conditions"][3]["value"].as_array().unwrap();
        assert_eq!(spenders.len(), d.kuru.markets.len() + 5 + 2 + 8);
        assert!(spenders.contains(&json!(lower(&d.lending.aave_v3.pool))));
        assert!(!r[0]["conditions"][1]["value"].as_array().unwrap().contains(&json!(lower(&Address::ZERO))), "native MON is no approve target");
        assert_eq!(r[1]["conditions"][3]["value"], TREASURY_PLACEHOLDER);
    }

    #[test]
    fn treasury_pins_transfer_recipient() {
        let mut d = dep();
        d.treasury = "0x7777777777777777777777777777777777777777".into();
        let r = rules(&d);
        assert_eq!(r[1]["conditions"][1]["value"], json!([lower(&d.tokens["USDC"].address)]));
        assert_eq!(r[1]["conditions"][3]["value"], "0x7777777777777777777777777777777777777777");
    }
}
