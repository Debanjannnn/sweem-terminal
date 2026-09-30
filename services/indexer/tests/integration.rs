//! Needs Postgres (localhost:5433 or DATABASE_URL) with migrations ≤0009 and ENVIO_API_TOKEN (env or main/.env).
//! Run: cargo test --test integration -- --ignored
//! Writes only a throwaway user/delegation (did:itest:indexer) and rows under cursor `itest`; cleans both up.
use indexer::{config::Config, store, Indexer};

const ROOT: &str = concat!(env!("CARGO_MANIFEST_DIR"), "/../..");
/// Active Kuru MON/USDC maker+taker (from mainnet Trade logs, blocks 109193624..109194396).
const WALLET: &str = "0x34ae4d081c0d46b23ebf5292c1be4e45f8a73fc5";
const FROM: u64 = 109_193_600;
const TO: u64 = 109_194_400;
const DID: &str = "did:itest:indexer";

fn cfg() -> Config {
    if std::env::var("ENVIO_API_TOKEN").is_err() {
        let env = std::fs::read_to_string(format!("{ROOT}/.env")).unwrap();
        let v = env
            .lines()
            .find_map(|l| l.strip_prefix("ENVIO_API_TOKEN="))
            .expect("ENVIO_API_TOKEN");
        std::env::set_var("ENVIO_API_TOKEN", v.trim_matches('"'));
    }
    std::env::set_var(
        "DEPLOYMENTS_PATH",
        format!("{ROOT}/contracts/deployments/143.json"),
    );
    if std::env::var("DATABASE_URL").is_err() {
        std::env::set_var(
            "DATABASE_URL",
            "postgres://sweem:sweem@localhost:5433/sweem?sslmode=disable",
        );
    }
    let mut c = Config::from_env().unwrap();
    c.start_block = FROM;
    c
}

async fn cleanup(db: &tokio_postgres::Client) {
    db.batch_execute(&format!(
        "delete from delegations where user_id = '{DID}';
         delete from users where id = '{DID}';
         delete from indexer_cursor where name = 'itest';
         delete from indexed_wallets where name = 'itest';
         delete from user_trades where wallet = '{WALLET}' and block between {FROM} and {TO};
         delete from user_orders_events where wallet = '{WALLET}' and block between {FROM} and {TO};"
    ))
    .await
    .unwrap();
}

async fn run(cfg: Config) {
    let mut ix = Indexer::new(cfg, "itest").await.unwrap();
    let db = store::connect(&std::env::var("DATABASE_URL").unwrap())
        .await
        .unwrap();
    while store::cursor(&db, "itest").await.unwrap().unwrap_or(0) < TO {
        ix.step().await.unwrap();
    }
}

async fn counts(db: &tokio_postgres::Client) -> Vec<i64> {
    let mut v = Vec::new();
    for t in ["user_trades", "user_orders_events"] {
        let q =
            format!("select count(*) from {t} where wallet = $1 and block between {FROM} and {TO}");
        v.push(db.query_one(&q, &[&WALLET]).await.unwrap().get(0));
    }
    v
}

#[tokio::test(flavor = "multi_thread")]
#[ignore]
async fn indexes_known_wallet_idempotently() {
    let cfg = cfg();
    let db = store::connect(&cfg.database_url).await.unwrap();
    db.batch_execute(
        &std::fs::read_to_string(format!("{ROOT}/migrations/0010_indexer_mainnet.sql")).unwrap(),
    )
    .await
    .unwrap();
    cleanup(&db).await;
    db.batch_execute(&format!(
        "insert into users (id, wallet_address) values ('{DID}', '{WALLET}');
         insert into delegations (user_id, privy_wallet_id, wallet_address, key_quorum_id, active)
         values ('{DID}', 'itest', '{WALLET}', 'itest', false);"
    ))
    .await
    .unwrap();

    run(cfg).await;
    // taker buy sweeping two asks at block 109194076 (isBuy=true)
    let r = db
        .query(
            "select side, role, price::text, size::text from user_trades where wallet = $1 and block = 109194076 order by log_index",
            &[&WALLET],
        )
        .await
        .unwrap();
    let got: Vec<(String, String, String, String)> = r
        .iter()
        .map(|r| (r.get(0), r.get(1), r.get(2), r.get(3)))
        .collect();
    assert_eq!(
        got,
        [
            (
                "buy".into(),
                "taker".into(),
                "27049000000000000".into(),
                "2000000000000".into()
            ),
            (
                "buy".into(),
                "taker".into(),
                "27050000000000000".into(),
                "2000000000000".into()
            ),
        ]
    );
    // order 0x6ccf0c6 created then canceled in tx at 109193624
    let r = db
        .query(
            "select kind, side from user_orders_events where wallet = $1 and order_id = $2 order by log_index",
            &[&WALLET, &0x6ccf0c6i64],
        )
        .await
        .unwrap();
    let got: Vec<(String, Option<String>)> = r.iter().map(|r| (r.get(0), r.get(1))).collect();
    assert_eq!(
        got,
        [
            ("created".into(), Some("buy".into())),
            ("canceled".into(), None)
        ]
    );

    // replay from scratch writes nothing new
    let first = counts(&db).await;
    db.execute("delete from indexer_cursor where name = 'itest'", &[])
        .await
        .unwrap();
    run(self::cfg()).await;
    assert_eq!(counts(&db).await, first);
    cleanup(&db).await;
}
