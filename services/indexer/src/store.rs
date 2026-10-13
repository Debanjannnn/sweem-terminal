use crate::decode::Venue;
use anyhow::Result;
use std::collections::{BTreeSet, HashMap};
use tokio_postgres::{Client, NoTls};

pub struct Row {
    pub tx_hash: String,
    pub log_index: i32,
    pub block: i64,
    pub ts: i64,
    /// Emitting contract.
    pub contract: String,
    pub rec: Rec,
}

#[derive(Debug, PartialEq)]
pub enum Rec {
    Trade {
        role: &'static str,
        wallet: String,
        side: &'static str,
        order_id: i64,
        price: String,
        size: String,
        updated_size: String,
    },
    Order {
        order_id: i64,
        wallet: String,
        kind: &'static str,
        side: Option<&'static str>,
        price: Option<String>,
        size: Option<String>,
    },
    PerplAccount {
        id: String,
        wallet: String,
    },
    /// `account` already resolved to the wallet.
    Venue(Venue),
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

/// Age in seconds of the oldest delegated wallet not yet in `indexed_wallets` (by its user's signup), if any.
pub async fn new_wallet_age(db: &Client, name: &str) -> Result<Option<f64>> {
    let r = db
        .query_one(
            "select extract(epoch from now() - min(coalesce(u.created_at, d.created_at)))::float8 from delegations d
             left join users u on u.id = d.user_id
             where lower(d.wallet_address) not in (select wallet from indexed_wallets where name = $1)",
            &[&name],
        )
        .await?;
    Ok(r.get(0))
}

/// Our users' wallets. Newly added wallets rewind `name` to `rewind` (or startBlock when None) so their
/// history is picked up; replays are idempotent.
pub async fn sync_wallets(
    db: &mut Client,
    name: &str,
    rewind: Option<u64>,
) -> Result<BTreeSet<String>> {
    let tx = db.transaction().await?;
    let added = tx
        .execute(
            "insert into indexed_wallets (name, wallet) select distinct $1, lower(wallet_address) from delegations on conflict do nothing",
            &[&name],
        )
        .await?;
    if added > 0 {
        match rewind {
            Some(b) => {
                tx.execute(
                    "update indexer_cursor set block = least(block, $2) where name = $1",
                    &[&name, &(b as i64)],
                )
                .await?
            }
            None => {
                tx.execute("delete from indexer_cursor where name = $1", &[&name])
                    .await?
            }
        };
    }
    let rows = tx
        .query(
            "select wallet from indexed_wallets where name = $1",
            &[&name],
        )
        .await?;
    tx.commit().await?;
    Ok(rows.iter().map(|r| r.get(0)).collect())
}

pub async fn perpl_accounts(db: &Client) -> Result<HashMap<String, String>> {
    let rows = db
        .query("select account_id::text, wallet from perpl_accounts", &[])
        .await?;
    Ok(rows.iter().map(|r| (r.get(0), r.get(1))).collect())
}

/// Writes rows and advances the cursor atomically.
pub async fn write(db: &mut Client, name: &str, rows: &[Row], cursor: u64) -> Result<()> {
    let tx = db.transaction().await?;
    for r in rows {
        let (h, i, b, ts, c) = (&r.tx_hash, &r.log_index, &r.block, &r.ts, &r.contract);
        match &r.rec {
            Rec::Trade {
                role,
                wallet,
                side,
                order_id,
                price,
                size,
                updated_size,
            } => {
                tx.execute(
                    "insert into user_trades (tx_hash, log_index, role, market, wallet, side, order_id, price, size, updated_size, block, ts)
                     values ($1, $2, $3, $4, $5, $6, $7, $8::text::numeric, $9::text::numeric, $10::text::numeric, $11, to_timestamp($12::bigint))
                     on conflict do nothing",
                    &[h, i, role, c, wallet, side, order_id, price, size, updated_size, b, ts],
                )
                .await?;
            }
            Rec::Order {
                order_id,
                wallet,
                kind,
                side,
                price,
                size,
            } => {
                tx.execute(
                    "insert into user_orders_events (tx_hash, log_index, order_id, market, wallet, kind, side, price, size, block, ts)
                     values ($1, $2, $3, $4, $5, $6, $7, $8::text::numeric, $9::text::numeric, $10, to_timestamp($11::bigint))
                     on conflict do nothing",
                    &[h, i, order_id, c, wallet, kind, side, price, size, b, ts],
                )
                .await?;
            }
            Rec::PerplAccount { id, wallet } => {
                tx.execute(
                    "insert into perpl_accounts (account_id, wallet) values ($1::text::numeric, $2) on conflict do nothing",
                    &[id, wallet],
                )
                .await?;
            }
            Rec::Venue(v) => {
                tx.execute(
                    "insert into venue_events (tx_hash, log_index, venue, contract, kind, wallet, asset, amount, shares, extra, block, ts)
                     values ($1, $2, $3, $4, $5, $6, $7, $8::text::numeric, $9::text::numeric, $10, $11, to_timestamp($12::bigint))
                     on conflict do nothing",
                    &[h, i, &v.venue, c, &v.kind, &v.account, &v.asset, &v.amount, &v.shares, &v.extra, b, ts],
                )
                .await?;
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
