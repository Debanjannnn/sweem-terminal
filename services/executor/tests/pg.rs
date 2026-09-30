//! real postgres with migration 0007 applied + mainnet RPC. never reaches Privy: the one plan that runs reverts at estimation.
//! run: DATABASE_URL=… RPC_URL=https://rpc.monad.xyz cargo test --test pg -- --ignored
use axum::{body::Body, http::Request};
use executor::{http, store};
use serde_json::Value;
use tower::ServiceExt;

async fn call(app: &http::App, caller: &str, secret: &str, body: &str) -> (u16, Value) {
    let req = Request::post("/api/v1/user/steps")
        .header("content-type", "application/json")
        .header("x-caller", caller)
        .header("x-internal-secret", secret)
        .body(Body::from(body.to_string()))
        .unwrap();
    let resp = http::router(app.clone()).oneshot(req).await.unwrap();
    let status = resp.status().as_u16();
    let bytes = axum::body::to_bytes(resp.into_body(), 1 << 20).await.unwrap();
    (status, serde_json::from_slice(&bytes).unwrap_or(Value::Null))
}

const BUY: &str = r#"[{"action":"kuru.marketBuy","market":"WETH","quote":"100000000","minOut":"1"}]"#;

#[tokio::test]
#[ignore]
async fn execution_state_machine() {
    let app = executor::app_from_env().await.unwrap();
    let secret = app.secret.clone();
    let db = std::sync::Arc::new(app.db.get().await.unwrap());
    let user = format!("did:privy:executor-test-{}", std::process::id());

    db.execute("insert into users(id) values ($1)", &[&user]).await.unwrap();
    db.execute(
        "insert into delegations(user_id, privy_wallet_id, wallet_address, key_quorum_id) values ($1,'w-test','0x0000000000000000000000000000000000000001','q')",
        &[&user],
    )
    .await
    .unwrap();
    let mut slot = 0i64;
    let mut exec = |steps: &str| {
        slot += 1;
        let (db, user, steps) = (db.clone(), user.clone(), steps.to_string());
        async move {
            db.query_one(
                "insert into executions(user_id,slot,kind,steps) values ($1,$2,'basket_buy',$3::text::jsonb) returning id::text",
                &[&user, &slot, &steps],
            )
            .await
            .unwrap()
            .get::<_, String>(0)
        }
    };
    let body = |id: &str| format!(r#"{{"executionId":"{id}"}}"#);
    let row = |id: String| {
        let db = db.clone();
        async move {
            let r = db.query_one("select status, error, step_done from executions where id = $1::text::uuid", &[&id]).await.unwrap();
            (r.get::<_, String>(0), r.get::<_, Option<String>>(1), r.get::<_, i32>(2))
        }
    };

    // auth
    let id = exec(BUY).await;
    assert_eq!(call(&app, "scheduler", "wrong", &body(&id)).await.0, 401);
    assert_eq!(call(&app, "seeder", &secret, &body(&id)).await.0, 401);
    assert_eq!(row(id).await.0, "pending", "unauthorized must not touch the row");

    // cap: 1001 USDC > default 1000
    let id = exec(r#"[{"action":"lend.supply","venue":"aaveV3","asset":"USDC","amount":"1001000000"}]"#).await;
    let (code, v) = call(&app, "api", &secret, &body(&id)).await;
    assert_eq!((code, v["error"].as_str()), (409, Some("cap_exceeded")));
    assert_eq!(row(id.clone()).await, ("rejected".into(), Some("cap_exceeded".into()), 0));
    let (code, v) = call(&app, "api", &secret, &body(&id)).await;
    assert_eq!((code, v["error"].as_str()), (409, Some("not_pending")), "replay is a no-op");

    // off-allowlist target
    let id = exec(r#"[{"action":"vault.deposit","vault":"0x2222222222222222222222222222222222222222","amount":"1"}]"#).await;
    assert_eq!(call(&app, "scheduler", &secret, &body(&id)).await.1["error"], "bad_request");
    assert_eq!(row(id).await.0, "rejected");

    // inactive schedule
    let s: String = db.query_one(
        "insert into schedules(user_id,basket,kind,amount_usdc,interval_seconds,next_run_at,max_slippage_bps,max_per_run_usdc,active)
         values ($1,'WETH','recurring',10,86400,now(),50,10,false) returning id::text",
        &[&user],
    ).await.unwrap().get(0);
    let id: String = db.query_one(
        "insert into executions(schedule_id,slot,kind,steps) values ($1::text::uuid,1,'dca',$2::text::jsonb) returning id::text",
        &[&s, &BUY],
    ).await.unwrap().get(0);
    assert_eq!(call(&app, "scheduler", &secret, &body(&id)).await.1["error"], "schedule_inactive");

    // a plan that runs: step 0 reverts at estimation (wallet 0x..01 holds no shMON) → failed, partial progress 0, Privy never called
    let id = exec(r#"[{"action":"shmonad.redeem","shares":"1000000000000000000"},{"action":"shmonad.redeem","shares":"1"}]"#).await;
    let (code, v) = call(&app, "scheduler", &secret, &body(&id)).await;
    assert_eq!(code, 200, "{v}");
    assert_eq!((v["status"].as_str(), v["stepsDone"].as_u64(), v["error"].as_str()), (Some("failed"), Some(0), Some("chain_error")));
    assert_eq!(row(id.clone()).await, ("failed".into(), Some("chain_error".into()), 0));
    assert_eq!(call(&app, "scheduler", &secret, &body(&id)).await.1["error"], "not_pending");

    // progress bookkeeping + restart recovery
    let id = exec(BUY).await;
    assert!(store::transition(&app.db, &id, "pending", "submitted", None).await.unwrap());
    assert!(!store::transition(&app.db, &id, "pending", "submitted", None).await.unwrap(), "second claim loses");
    store::step_done(&app.db, &id, "0xabc").await.unwrap();
    store::fail_interrupted(&app.db).await.unwrap();
    assert_eq!(row(id.clone()).await, ("failed".into(), Some("interrupted".into()), 1));
    let h: Vec<String> = db.query_one("select tx_hashes from executions where id = $1::text::uuid", &[&id]).await.unwrap().get(0);
    assert_eq!(h, vec!["0xabc"]);

    // revoked delegation
    db.execute("update delegations set active = false, revoked_at = now() where user_id = $1", &[&user]).await.unwrap();
    let id = exec(BUY).await;
    assert_eq!(call(&app, "scheduler", &secret, &body(&id)).await.1["error"], "delegation_inactive");
    assert_eq!(row(id).await, ("rejected".into(), Some("delegation_inactive".into()), 0));

    assert_eq!(call(&app, "scheduler", &secret, &body("00000000-0000-0000-0000-000000000000")).await.0, 404);
    assert_eq!(call(&app, "scheduler", &secret, r#"{"nope":1}"#).await.0, 400);
    assert_eq!(call(&app, "scheduler", &secret, &body("not-a-uuid")).await.0, 400);

    for q in [
        "delete from executions where user_id = $1 or schedule_id in (select id from schedules where user_id = $1)",
        "delete from schedules where user_id = $1",
        "delete from delegations where user_id = $1",
        "delete from users where id = $1",
    ] {
        db.execute(q, &[&user]).await.unwrap();
    }
}
