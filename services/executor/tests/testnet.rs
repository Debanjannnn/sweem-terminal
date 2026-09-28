//! REAL Monad testnet, spends bot gas + 20 USDC deposit. run: cargo test --test testnet -- --ignored --nocapture
use axum::{body::Body, http::Request};
use executor::http;
use serde_json::{json, Value};
use tower::ServiceExt;

async fn call(app: &http::App, method: &str, path: &str, body: Value) -> (u16, Value) {
    let req = Request::builder()
        .method(method)
        .uri(path)
        .header("content-type", "application/json")
        .header("x-caller", "seeder")
        .header("x-internal-secret", app.secret.clone())
        .body(if body.is_null() { Body::empty() } else { Body::from(body.to_string()) })
        .unwrap();
    let resp = http::router(app.clone()).oneshot(req).await.unwrap();
    let status = resp.status().as_u16();
    let bytes = axum::body::to_bytes(resp.into_body(), 1 << 20).await.unwrap();
    (status, serde_json::from_slice(&bytes).unwrap_or(Value::Null))
}

const WETH_USDC: &str = "0xa9c2936656a7d2143720bcd91ba8506200b7cbe7";

#[tokio::test]
#[ignore]
async fn bot_deposit_order_cancel() {
    let _ =tracing_subscriber::fmt().with_env_filter("executor=debug").try_init();
    let app = executor::app_from_env().await.unwrap();
    let usdc = app.dep.kuru.usdc.to_string().to_lowercase();

    let (code, v) = call(&app, "POST", "/api/v1/bot/deposit", json!({"token": usdc, "amount": "20000000"})).await;
    assert_eq!(code, 200, "{v}");
    let dep = v["txHash"].as_str().unwrap().to_string();
    println!("deposit tx {dep}");
    assert!(app.rpc.wait_receipt(&dep, std::time::Duration::from_secs(30)).await.unwrap().ok());

    let (code, bal) = call(&app, "GET", "/api/v1/bot/balances", Value::Null).await;
    assert_eq!(code, 200, "{bal}");
    println!("balances {bal}");
    assert_ne!(bal["accountId"], "0");

    // 0.01 WETH bid at 1000.00 USDC: 10 USDC notional (the minimum), far below the ~2700 mid
    let order = json!({"market": WETH_USDC, "cancels": [], "orders": [{"side": "buy", "price": "100000", "size": "100000000", "tif": "gtc"}]});
    let (code, v) = call(&app, "POST", "/api/v1/bot/orders", order).await;
    assert_eq!(code, 200, "{v}");
    println!("place tx {} {v}", v["txHash"]);
    assert_eq!(v["status"], "confirmed");
    let ids = v["orderIds"].as_array().unwrap().clone();
    assert_eq!(ids.len(), 1, "one resting order");

    // fresh app: empty slot cache, so the cancel resolves id→slot from chain
    let app2 = executor::app_from_env().await.unwrap();
    let (code, v) = call(&app2, "POST", "/api/v1/bot/orders", json!({"market": WETH_USDC, "cancels": ids, "orders": []})).await;
    assert_eq!(code, 200, "{v}");
    println!("cancel tx {} {v}", v["txHash"]);
    assert_eq!(v["status"], "confirmed");

    let (_, t) = call(&app2, "GET", &format!("/api/v1/tx/{}", v["txHash"].as_str().unwrap()), Value::Null).await;
    assert_eq!(t["status"], "confirmed", "{t}");

    let (_, after) = call(&app2, "GET", "/api/v1/bot/balances", Value::Null).await;
    println!("after {after}");
    let locked = after["balances"].as_array().unwrap().iter().find(|b| b["token"] == usdc.as_str()).unwrap()["locked"].clone();
    assert_eq!(locked, "0", "cancel released the bid's USDC reserve");

    // cancelling a gone id is a noop, not a paid empty tx
    let (code, v) = call(&app2, "POST", "/api/v1/bot/orders", json!({"market": WETH_USDC, "cancels": ids, "orders": []})).await;
    assert_eq!((code, v["status"].as_str()), (200, Some("noop")), "{v}");
}
