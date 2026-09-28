//! Needs `make infra` and ENVIO_API_TOKEN (env or main/.env). Run: cargo test --test integration -- --ignored
use indexer::{config::Config, store, Indexer};

const ROOT: &str = concat!(env!("CARGO_MANIFEST_DIR"), "/../..");

fn cfg() -> Config {
    let token = std::env::var("ENVIO_API_TOKEN").unwrap_or_else(|_| {
        let env = std::fs::read_to_string(format!("{ROOT}/.env")).unwrap();
        let line = env
            .lines()
            .find(|l| l.starts_with("ENVIO_API_TOKEN="))
            .expect("ENVIO_API_TOKEN");
        line["ENVIO_API_TOKEN=".len()..]
            .trim_matches('"')
            .to_string()
    });
    std::env::set_var("ENVIO_API_TOKEN", token);
    std::env::set_var(
        "DEPLOYMENTS_PATH",
        format!("{ROOT}/contracts/deployments/10143.json"),
    );
    if std::env::var("DATABASE_URL").is_err() {
        std::env::set_var(
            "DATABASE_URL",
            "postgres://sweem:sweem@localhost:5433/sweem?sslmode=disable",
        );
    }
    Config::from_env().unwrap()
}

/// Row counts up to `upto`, so blocks finalized between runs don't count as a diff.
async fn counts(db: &tokio_postgres::Client, upto: i64) -> Vec<i64> {
    let mut v = Vec::new();
    for q in [
        "select count(*) from baskets where created_block <= $1",
        "select count(*) from market_allowlist where updated_block <= $1",
        "select count(*) from mints where block <= $1",
        "select count(*) from redeems where block <= $1",
        "select count(*) from kuru_trades where block <= $1",
    ] {
        v.push(db.query_one(q, &[&upto]).await.unwrap().get(0));
    }
    v
}

async fn run_to_head(cfg: &Config) {
    let mut ix = Indexer::new(cfg, "itest").await.unwrap();
    while ix.step().await.unwrap() {}
}

#[tokio::test(flavor = "multi_thread")]
#[ignore]
async fn indexes_from_start_block_idempotently() {
    let cfg = cfg();
    let db = store::connect(&cfg.database_url).await.unwrap();
    db.execute("delete from indexer_cursor where name = 'itest'", &[])
        .await
        .unwrap();

    run_to_head(&cfg).await;
    let blue = db
        .query_one("select symbol, jsonb_array_length(components) from baskets where address = '0xe0b3baafa8edd1db95b49e62ea0fb18c04353870'", &[])
        .await
        .unwrap();
    assert_eq!(blue.get::<_, String>(0), "BLUE");
    assert!(blue.get::<_, i32>(1) > 0);
    let n: i64 = db
        .query_one("select count(*) from market_allowlist where allowed", &[])
        .await
        .unwrap()
        .get(0);
    assert_eq!(n, 4);
    let upto = store::cursor(&db, "itest").await.unwrap().unwrap();
    assert!(upto >= cfg.start_block);
    let first = counts(&db, upto as i64).await;

    db.execute("delete from indexer_cursor where name = 'itest'", &[])
        .await
        .unwrap();
    run_to_head(&cfg).await;
    assert_eq!(counts(&db, upto as i64).await, first);
    db.execute("delete from indexer_cursor where name = 'itest'", &[])
        .await
        .unwrap();
}
