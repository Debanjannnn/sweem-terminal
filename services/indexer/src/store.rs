use crate::decode::Event;
use anyhow::Result;
use std::collections::BTreeSet;
use tokio_postgres::{Client, NoTls};

pub struct Row {
    pub tx_hash: String,
    pub log_index: i32,
    pub block: i64,
    pub ts: i64,
    pub from: Option<String>,
    pub event: Event,
}

pub async fn connect(url: &str) -> Result<Client> {
    let (client, conn) = tokio_postgres::connect(url, NoTls).await?;
    tokio::spawn(async move {
        if let Err(e) = conn.await {
            eprintln!("postgres connection: {e}");
        }
    });
    Ok(client)
}

pub async fn cursor(db: &Client, name: &str) -> Result<Option<u64>> {
    let r = db
        .query_opt("select block from indexer_cursor where name = $1", &[&name])
        .await?;
    Ok(r.map(|r| r.get::<_, i64>(0) as u64))
}

pub async fn known(db: &Client) -> Result<(BTreeSet<String>, BTreeSet<String>)> {
    let col = |rows: Vec<tokio_postgres::Row>| rows.iter().map(|r| r.get(0)).collect();
    Ok((
        col(db.query("select address from baskets", &[]).await?),
        col(db.query("select market from market_allowlist", &[]).await?),
    ))
}

/// Rewinds `name` to startBlock while any basket lacks a creator; re-processing is idempotent and fills it.
pub async fn backfill_creators(db: &Client, name: &str) -> Result<()> {
    db.execute(
        "delete from indexer_cursor where name = $1 and exists (select 1 from baskets where creator is null)",
        &[&name],
    )
    .await?;
    Ok(())
}

/// Writes rows (in chain order) and advances the cursor atomically.
pub async fn write(db: &mut Client, name: &str, rows: &[Row], cursor: u64) -> Result<()> {
    let tx = db.transaction().await?;
    for r in rows {
        let (h, i, b, ts) = (&r.tx_hash, &r.log_index, &r.block, &r.ts);
        match &r.event {
            Event::MarketAllowed { market, allowed } => {
                tx.execute(
                    "insert into market_allowlist (market, allowed, updated_block) values ($1, $2, $3)
                     on conflict (market) do update set allowed = excluded.allowed, updated_block = excluded.updated_block
                     where market_allowlist.updated_block <= excluded.updated_block",
                    &[market, allowed, b],
                )
                .await?;
            }
            Event::BasketCreated {
                basket,
                name,
                symbol,
                components,
            } => {
                tx.execute(
                    "insert into baskets (address, name, symbol, components, created_block, created_tx, creator)
                     values ($1, $2, $3, $4, $5, $6, $7)
                     on conflict (address) do update set creator = coalesce(excluded.creator, baskets.creator)",
                    &[basket, name, symbol, components, b, h, &r.from],
                )
                .await?;
            }
            Event::Minted {
                basket,
                account,
                usdc,
                shares,
            } => {
                tx.execute(
                    "insert into mints (tx_hash, log_index, basket, account, usdc, shares, block, ts)
                     values ($1, $2, $3, $4, $5::text::numeric, $6::text::numeric, $7, to_timestamp($8::bigint)) on conflict do nothing",
                    &[h, i, basket, account, usdc, shares, b, ts],
                )
                .await?;
            }
            Event::Redeemed {
                basket,
                account,
                shares,
                usdc,
            } => {
                tx.execute(
                    "insert into redeems (tx_hash, log_index, basket, account, shares, usdc, block, ts)
                     values ($1, $2, $3, $4, $5::text::numeric, $6::text::numeric, $7, to_timestamp($8::bigint)) on conflict do nothing",
                    &[h, i, basket, account, shares, usdc, b, ts],
                )
                .await?;
            }
            Event::Trades { market, trades } => {
                for (idx, t) in trades.iter().enumerate() {
                    tx.execute(
                        "insert into kuru_trades (tx_hash, log_index, idx, market, price, size, side, block, ts)
                         values ($1, $2, $3, $4, $5::text::numeric, $6::text::numeric, $7, $8, to_timestamp($9::bigint)) on conflict do nothing",
                        &[h, i, &(idx as i32), market, &t.price.to_string(), &t.size.to_string(), &t.side, b, ts],
                    )
                    .await?;
                }
            }
        }
    }
    tx.execute(
        "insert into indexer_cursor (name, block) values ($1, $2) on conflict (name) do update set block = excluded.block",
        &[&name, &(cursor as i64)],
    )
    .await?;
    tx.commit().await?;
    Ok(())
}
