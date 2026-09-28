//! real postgres (`make infra`). run: cargo test --test pg -- --ignored
use axum::{body::Body, http::Request};
use executor::{http, store};
// REVERTED_TX=<hash of a reverted testnet tx> also covers the failed branch
use serde_json::Value;
use tower::ServiceExt;

// the bot's real 20 USDC AccountCore deposit on testnet
const CONFIRMED_TX: &str = "0xa3696ea3f8c34773142ea93eb3136b9c3569559d7a9d3ad97a377d314e361d45";

async fn call(app: &http::App, caller: &str, secret: &str, path: &str, body: &str) -> (u16, Value) {
    let req = Request::post(path)
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
    let sched = |active: bool| {
        let (db, user) = (db.clone(), user.clone());
        async move {
            let r = db.query_one(
                "insert into schedules(user_id,basket,kind,amount_usdc,interval_seconds,next_run_at,max_slippage_bps,max_per_run_usdc,active)
                 values ($1,'0xe0b3baafa8edd1db95b49e62ea0fb18c04353870','recurring',10,86400,now(),50,10,$2) returning id::text",
                &[&user, &active],
            ).await.unwrap();
            r.get::<_, String>(0)
        }
    };
    let (active, inactive) = (sched(true).await, sched(false).await);
    let mut slot = 0i64;
    let mut exec = |s: &str, kind: &str, amount: &str| {
        slot += 1;
        let (db, s, kind, amount) = (db.clone(), s.to_string(), kind.to_string(), amount.to_string());
        async move {
            let r = db.query_one(
                "insert into executions(schedule_id,slot,kind,amount,min_out) values ($1::text::uuid,$2,$3,$4::text::numeric,0) returning id::text",
                &[&s, &slot, &kind, &amount],
            ).await.unwrap();
            r.get::<_, String>(0)
        }
    };
    let body = |id: &str| format!(r#"{{"executionId":"{id}"}}"#);
    let row = |id: String| {
        let db = db.clone();
        async move {
            let r = db.query_one("select status, error from executions where id = $1::text::uuid", &[&id]).await.unwrap();
            (r.get::<_, String>(0), r.get::<_, Option<String>>(1))
        }
    };

    // auth
    let id = exec(&active, "mint", "20000000").await;
    assert_eq!(call(&app, "scheduler", "wrong", "/api/v1/user/mint", &body(&id)).await.0, 401);
    assert_eq!(call(&app, "seeder", &secret, "/api/v1/user/mint", &body(&id)).await.0, 401);
    assert_eq!(row(id.clone()).await.0, "pending", "unauthorized must not touch the row");

    // cap: 20 USDC > max_per_run 10
    let (code, v) = call(&app, "scheduler", &secret, "/api/v1/user/mint", &body(&id)).await;
    assert_eq!((code, v["error"].as_str()), (409, Some("cap_exceeded")));
    assert_eq!(row(id.clone()).await, ("rejected".into(), Some("cap_exceeded".into())));
    // replay is a no-op
    let (code, v) = call(&app, "scheduler", &secret, "/api/v1/user/mint", &body(&id)).await;
    assert_eq!((code, v["error"].as_str()), (409, Some("not_pending")));
    assert_eq!(row(id).await, ("rejected".into(), Some("cap_exceeded".into())));

    // kind must match the route
    let id = exec(&active, "redeem", "1").await;
    assert_eq!(call(&app, "scheduler", &secret, "/api/v1/user/mint", &body(&id)).await.1["error"], "bad_request");
    assert_eq!(row(id).await.0, "rejected");

    let id = exec(&inactive, "mint", "1000000").await;
    assert_eq!(call(&app, "scheduler", &secret, "/api/v1/user/mint", &body(&id)).await.1["error"], "schedule_inactive");

    // a fired stop-loss is inactive by design (scheduler's fire-once claim); its redeem must still pass
    let sl: String = db.query_one(
        "insert into schedules(user_id,basket,kind,stop_nav_per_share,redeem_bps,max_slippage_bps,max_per_run_usdc,active)
         values ($1,'0xe0b3baafa8edd1db95b49e62ea0fb18c04353870','stop_loss',1,5000,50,10,false) returning id::text",
        &[&user],
    ).await.unwrap().get(0);
    let id = exec(&sl, "redeem", "1").await;
    assert!(store::load(&app.db, &id).await.unwrap().unwrap().schedule_active);

    // conditional transitions: the path a real send takes
    let id = exec(&active, "mint", "1000000").await;
    assert!(store::transition(&app.db, &id, "pending", "submitted", None).await.unwrap());
    assert!(!store::transition(&app.db, &id, "pending", "submitted", None).await.unwrap(), "second claim loses");
    store::set_tx(&app.db, &id, "0xabc").await.unwrap();
    assert!(store::transition(&app.db, &id, "submitted", "confirmed", None).await.unwrap());
    assert!(!store::transition(&app.db, &id, "submitted", "failed", Some("reverted")).await.unwrap());
    assert_eq!(row(id.clone()).await, ("confirmed".into(), None));
    assert_eq!(call(&app, "scheduler", &secret, "/api/v1/user/mint", &body(&id)).await.1["error"], "not_pending");

    // revoked delegation
    db.execute("update delegations set active = false, revoked_at = now() where user_id = $1", &[&user]).await.unwrap();
    let id = exec(&active, "mint", "1000000").await;
    assert_eq!(call(&app, "scheduler", &secret, "/api/v1/user/mint", &body(&id)).await.1["error"], "delegation_inactive");
    assert_eq!(row(id).await, ("rejected".into(), Some("delegation_inactive".into())));

    // restart recovery: a submitted row with a mined hash is resolved by resume()
    let id = exec(&active, "mint", "1000000").await;
    assert!(store::transition(&app.db, &id, "pending", "submitted", None).await.unwrap());
    store::set_tx(&app.db, &id, CONFIRMED_TX).await.unwrap();
    let failed = match std::env::var("REVERTED_TX") {
        Ok(h) => {
            let f = exec(&active, "mint", "1000000").await;
            assert!(store::transition(&app.db, &f, "pending", "submitted", None).await.unwrap());
            store::set_tx(&app.db, &f, &h).await.unwrap();
            Some(f)
        }
        Err(_) => None,
    };
    for h in executor::user::resume(&app).await.unwrap() {
        h.await.unwrap();
    }
    assert_eq!(row(id).await, ("confirmed".into(), None));
    if let Some(f) = failed {
        assert_eq!(row(f).await, ("failed".into(), Some("reverted".into())));
    }

    let unknown = "00000000-0000-0000-0000-000000000000";
    assert_eq!(call(&app, "scheduler", &secret, "/api/v1/user/redeem", &body(unknown)).await.0, 404);
    assert_eq!(call(&app, "scheduler", &secret, "/api/v1/user/redeem", r#"{"nope":1}"#).await.0, 400);
    assert_eq!(call(&app, "scheduler", &secret, "/api/v1/user/redeem", &body("not-a-uuid")).await.0, 400);

    for q in [
        "delete from executions where schedule_id in (select id from schedules where user_id = $1)",
        "delete from schedules where user_id = $1",
        "delete from delegations where user_id = $1",
        "delete from users where id = $1",
    ] {
        db.execute(q, &[&user]).await.unwrap();
    }
}
