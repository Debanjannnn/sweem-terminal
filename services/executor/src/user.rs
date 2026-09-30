//! runs a planned execution's steps from the user's Privy wallet, in order. re-checks everything; decides nothing.
use alloy::primitives::{Address, U256};
use axum::http::StatusCode;

use crate::{
    config::Deployments,
    http::{App, Fail},
    signer::Signer,
    steps::{self, Call, Step},
    store::{self, Execution},
};

pub const MAX_STEPS: usize = 32;

#[derive(Debug, Clone, PartialEq, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Outcome {
    pub execution_id: String,
    pub status: String,
    pub steps_done: usize,
    pub tx_hashes: Vec<String>,
    pub error: Option<String>,
}

/// every step must build (known venue, allowlisted target, wallet as receiver) and fit the cap before anything is signed.
pub fn recheck(e: &Execution, dep: &Deployments, max_step_usd: u64) -> Result<(Address, Vec<Call>), &'static str> {
    if e.status != "pending" {
        return Err("not_pending");
    }
    let (Some(_), Some(addr)) = (&e.wallet_id, &e.wallet_address) else { return Err("delegation_inactive") };
    if !e.schedule_active {
        return Err("schedule_inactive");
    }
    let wallet: Address = addr.parse().map_err(|_| "bad_request")?;
    let plan: Vec<Step> = serde_json::from_str(e.steps.as_deref().ok_or("bad_request")?).map_err(|_| "bad_request")?;
    if plan.is_empty() || plan.len() > MAX_STEPS {
        return Err("bad_request");
    }
    let calls = plan.iter().map(|s| steps::build(dep, wallet, s)).collect::<Result<Vec<_>, _>>().map_err(|err| {
        tracing::debug!(%err, "step rejected");
        "bad_request"
    })?;
    let cap = U256::from(max_step_usd) * U256::from(1_000_000);
    if calls.iter().any(|c| steps::stable_spend(dep, c) > cap) {
        return Err("cap_exceeded");
    }
    Ok((wallet, calls))
}

pub async fn execute(app: &App, id: &str) -> Result<Outcome, Fail> {
    let db_err = |e: String| {
        tracing::error!(error = %e, "db");
        Fail(StatusCode::INTERNAL_SERVER_ERROR, "chain_error")
    };
    let e = store::load(&app.db, id).await.map_err(db_err)?.ok_or(Fail(StatusCode::NOT_FOUND, "bad_request"))?;
    let (wallet, calls) = match recheck(&e, &app.dep, app.max_step_usd) {
        Ok(v) => v,
        Err(code) => {
            if code != "not_pending" {
                store::transition(&app.db, id, "pending", "rejected", Some(code)).await.map_err(db_err)?;
            }
            return Err(Fail(StatusCode::CONFLICT, code));
        }
    };
    if !store::transition(&app.db, id, "pending", "submitted", None).await.map_err(db_err)? {
        return Err(Fail(StatusCode::CONFLICT, "not_pending"));
    }
    let wallet_id = e.wallet_id.unwrap_or_default();
    // own task: a caller that hangs up must not cancel a plan half-way
    let (app, id) = (app.clone(), id.to_string());
    tokio::spawn(async move {
        let _lease = app.leases.lease(&wallet_id).await;
        let signer = Signer::Privy(app.privy.clone(), wallet_id);
        let mut out = Outcome { execution_id: id.clone(), status: "confirmed".into(), steps_done: 0, tx_hashes: vec![], error: None };
        for c in &calls {
            match signer.perform(&app.rpc, wallet, c).await {
                Ok(h) => {
                    store::step_done(&app.db, &id, &h).await.map_err(db_err)?;
                    out.steps_done += 1;
                    out.tx_hashes.push(h);
                }
                Err(err) => {
                    tracing::warn!(execution = id, step = out.steps_done, error = %err, "step failed");
                    (out.status, out.error) = ("failed".into(), Some("chain_error".into()));
                    break;
                }
            }
        }
        store::transition(&app.db, &id, "submitted", &out.status, out.error.as_deref()).await.map_err(db_err)?;
        Ok(out)
    })
    .await
    .map_err(|_| Fail(StatusCode::INTERNAL_SERVER_ERROR, "chain_error"))?
}

#[cfg(test)]
mod tests {
    use super::*;

    fn dep() -> Deployments {
        Deployments::load(concat!(env!("CARGO_MANIFEST_DIR"), "/../../contracts/deployments/143.json")).unwrap()
    }

    fn ok() -> Execution {
        Execution {
            status: "pending".into(),
            steps: Some(r#"[{"action":"kuru.marketBuy","market":"WETH","quote":"100000000","minOut":"1"},{"action":"lend.supply","venue":"aaveV3","asset":"USDC","amount":"5000000"}]"#.into()),
            schedule_active: true,
            wallet_id: Some("w".into()),
            wallet_address: Some("0x0000000000000000000000000000000000000001".into()),
        }
    }

    #[test]
    fn recheck_matrix() {
        let d = dep();
        type Case = (fn(&mut Execution), Result<usize, &'static str>);
        let cases: Vec<Case> = vec![
            (|_| {}, Ok(2)),
            (|e| e.status = "submitted".into(), Err("not_pending")),
            (|e| e.status = "rejected".into(), Err("not_pending")),
            (|e| e.wallet_id = None, Err("delegation_inactive")),
            (|e| e.wallet_address = None, Err("delegation_inactive")),
            (|e| e.schedule_active = false, Err("schedule_inactive")),
            (|e| e.steps = None, Err("bad_request")),
            (|e| e.steps = Some("[]".into()), Err("bad_request")),
            (|e| e.steps = Some(r#"[{"action":"kuru.marketBuy","market":"PEPE","quote":"1","minOut":"0"}]"#.into()), Err("bad_request")),
            (|e| e.steps = Some(format!("[{}]", vec![r#"{"action":"shmonad.redeem","shares":"1"}"#; MAX_STEPS + 1].join(","))), Err("bad_request")),
            // 1000 USDC cap: exactly at cap passes, one atom over fails, on any stable
            (|e| e.steps = Some(r#"[{"action":"lend.supply","venue":"aaveV3","asset":"USDC","amount":"1000000000"}]"#.into()), Ok(1)),
            (|e| e.steps = Some(r#"[{"action":"lend.supply","venue":"aaveV3","asset":"USDC","amount":"1000000001"}]"#.into()), Err("cap_exceeded")),
            (|e| e.steps = Some(r#"[{"action":"stable.swap","from":"AUSD","to":"USDC","amount":"1000000001","minOut":"0"}]"#.into()), Err("cap_exceeded")),
            (|e| e.steps = Some(r#"[{"action":"lend.supply","venue":"aaveV3","asset":"WETH","amount":"1000000000000000000000"}]"#.into()), Ok(1)),
        ];
        for (f, want) in cases {
            let mut e = ok();
            f(&mut e);
            assert_eq!(recheck(&e, &d, 1000).map(|(_, c)| c.len()), want, "{e:?}");
        }
    }
}
