//! user mint/redeem through the user's Privy wallet. re-checks everything; decides nothing.
use alloy::{
    primitives::{hex, Address, U256},
    sol_types::{SolCall, SolError},
};
use axum::http::StatusCode;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use crate::{
    contracts::*,
    gas,
    http::{App, Fail},
    store::{self, Execution},
};

pub fn recheck(e: &Execution, route: &str) -> Result<(), &'static str> {
    if e.status != "pending" {
        return Err("not_pending");
    }
    if e.kind != route {
        return Err("bad_request");
    }
    if e.wallet_id.is_none() || e.wallet_address.is_none() {
        return Err("delegation_inactive");
    }
    if !e.schedule_active {
        return Err("schedule_inactive");
    }
    let amount = e.amount.as_deref().map(u256_str);
    let min_out = e.min_out.as_deref().map(u256_str);
    if !matches!(amount, Some(Ok(a)) if !a.is_zero()) || !matches!(min_out, Some(Ok(_))) {
        return Err("bad_request");
    }
    if route == "mint" && !e.within_cap {
        return Err("cap_exceeded");
    }
    Ok(())
}

fn chain_code(err: &str) -> &'static str {
    if err.contains(&hex::encode(Slippage::SELECTOR)) { "slippage" } else { "chain_error" }
}

pub async fn execute(app: &App, id: &str, route: &str) -> Result<String, Fail> {
    let db_err = |e: String| {
        tracing::error!(error = %e, "db");
        Fail(StatusCode::INTERNAL_SERVER_ERROR, "chain_error")
    };
    let e = store::load(&app.db, id).await.map_err(db_err)?.ok_or(Fail(StatusCode::NOT_FOUND, "bad_request"))?;
    if let Err(code) = recheck(&e, route) {
        if code != "not_pending" {
            store::transition(&app.db, id, "pending", "rejected", Some(code)).await.map_err(db_err)?;
        }
        return Err(Fail(StatusCode::CONFLICT, code));
    }
    if !store::transition(&app.db, id, "pending", "submitted", None).await.map_err(db_err)? {
        return Err(Fail(StatusCode::CONFLICT, "not_pending"));
    }
    match send(app, &e, route).await {
        Ok(hash) => {
            store::set_tx(&app.db, id, &hash).await.map_err(db_err)?;
            tokio::spawn(watch(app.db.clone(), app.rpc.clone(), id.to_string(), hash.clone()));
            Ok(hash)
        }
        Err(err) => {
            tracing::warn!(execution = id, error = %err, "user tx failed");
            let code = chain_code(&err);
            store::transition(&app.db, id, "submitted", "failed", Some(code)).await.map_err(db_err)?;
            Err(Fail(StatusCode::BAD_GATEWAY, code))
        }
    }
}

/// receipt → confirmed|failed. rows left `submitted` by a restart are re-watched by `resume`.
pub async fn watch(db: store::Db, rpc: std::sync::Arc<crate::rpc::Rpc>, id: String, hash: String) {
    let Some(r) = rpc.wait_receipt(&hash, Duration::from_secs(120)).await else {
        tracing::warn!(execution = id, tx = hash, "no receipt yet; resumed on next start");
        return;
    };
    let (to, err) = if r.ok() { ("confirmed", None) } else { ("failed", Some("reverted")) };
    if let Err(e) = store::transition(&db, &id, "submitted", to, err).await {
        tracing::error!(error = %e, "receipt status write");
    }
}

pub async fn resume(app: &App) -> Result<Vec<tokio::task::JoinHandle<()>>, String> {
    let rows = store::submitted(&app.db).await?;
    tracing::info!(count = rows.len(), "resuming receipt watch");
    Ok(rows.into_iter().map(|(id, h)| tokio::spawn(watch(app.db.clone(), app.rpc.clone(), id, h))).collect())
}

async fn send(app: &App, e: &Execution, route: &str) -> Result<String, String> {
    let wallet_id = e.wallet_id.as_deref().unwrap_or_default();
    let owner: Address = e.wallet_address.as_deref().unwrap_or_default().parse().map_err(|_| "bad wallet address")?;
    let basket: Address = e.basket.parse().map_err(|_| "bad basket address")?;
    let amount = u256_str(e.amount.as_deref().unwrap_or_default())?;
    let min_out = u256_str(e.min_out.as_deref().unwrap_or_default())?;
    let deadline = U256::from(SystemTime::now().duration_since(UNIX_EPOCH).unwrap().as_secs() + 300);
    let _lease = app.leases.lease(wallet_id).await;

    let data = if route == "mint" {
        let usdc = app.dep.kuru.usdc;
        let out = app.rpc.eth_call(usdc, &allowanceCall { owner, spender: basket }.abi_encode()).await?;
        if allowanceCall::abi_decode_returns(&out).map_err(|e| e.to_string())? < amount {
            let approve = approveCall { spender: basket, amount }.abi_encode();
            let fees = gas::fees(&app.rpc, owner, usdc, U256::ZERO, &approve).await?;
            let h = app.privy.send_transaction(wallet_id, usdc, &approve, &fees).await?;
            match app.rpc.wait_receipt(&h, Duration::from_secs(30)).await {
                Some(r) if r.ok() => {}
                _ => return Err(format!("approve {h} did not confirm")),
            }
        }
        mintCall { usdcIn: amount, minShares: min_out, deadline }.abi_encode()
    } else {
        redeemCall { shares: amount, minUsdcOut: min_out, deadline }.abi_encode()
    };
    let fees = gas::fees(&app.rpc, owner, basket, U256::ZERO, &data).await?;
    app.privy.send_transaction(wallet_id, basket, &data, &fees).await
}

#[cfg(test)]
mod tests {
    use super::*;

    fn ok() -> Execution {
        Execution {
            status: "pending".into(),
            kind: "mint".into(),
            amount: Some("10000000".into()),
            min_out: Some("0".into()),
            schedule_active: true,
            basket: "0xe0b3baafa8edd1db95b49e62ea0fb18c04353870".into(),
            within_cap: true,
            wallet_id: Some("w".into()),
            wallet_address: Some("0x0000000000000000000000000000000000000001".into()),
        }
    }

    #[test]
    fn recheck_matrix() {
        let cases: Vec<(&str, fn(&mut Execution), Result<(), &str>)> = vec![
            ("mint", |_| {}, Ok(())),
            ("redeem", |e| e.kind = "redeem".into(), Ok(())),
            ("redeem", |e| { e.kind = "redeem".into(); e.within_cap = false }, Ok(())),
            ("mint", |e| e.status = "submitted".into(), Err("not_pending")),
            ("mint", |e| e.status = "rejected".into(), Err("not_pending")),
            ("redeem", |_| {}, Err("bad_request")),
            ("mint", |e| e.wallet_id = None, Err("delegation_inactive")),
            ("mint", |e| e.schedule_active = false, Err("schedule_inactive")),
            ("mint", |e| e.amount = None, Err("bad_request")),
            ("mint", |e| e.amount = Some("0".into()), Err("bad_request")),
            ("mint", |e| e.amount = Some("1.5".into()), Err("bad_request")),
            ("mint", |e| e.min_out = None, Err("bad_request")),
            ("mint", |e| e.within_cap = false, Err("cap_exceeded")),
        ];
        for (route, f, want) in cases {
            let mut e = ok();
            f(&mut e);
            assert_eq!(recheck(&e, route), want, "{route} {e:?}");
        }
    }

    #[test]
    fn slippage_revert_is_named() {
        assert_eq!(chain_code(&format!("execution reverted data 0x{}", hex::encode(Slippage::SELECTOR))), "slippage");
        assert_eq!(chain_code("insufficient funds"), "chain_error");
    }
}
