pub mod config;
pub mod decode;
pub mod finality;
pub mod health;
pub mod source;
pub mod store;

use anyhow::{Context, Result};
use decode::Event;
use std::collections::BTreeSet;

pub struct Indexer {
    name: String,
    factory: String,
    start_block: u64,
    finality_blocks: u64,
    src: source::Source,
    db: tokio_postgres::Client,
    baskets: BTreeSet<String>,
    markets: BTreeSet<String>,
}

impl Indexer {
    /// `name` keys the row in `indexer_cursor`.
    pub async fn new(cfg: &config::Config, name: &str) -> Result<Self> {
        let db = store::connect(&cfg.database_url).await?;
        let (baskets, markets) = store::known(&db).await?;
        Ok(Indexer {
            name: name.into(),
            factory: cfg.factory.clone(),
            start_block: cfg.start_block,
            finality_blocks: cfg.finality_blocks,
            src: source::Source::new(&cfg.hypersync_url, &cfg.envio_token)?,
            db,
            baskets,
            markets,
        })
    }

    /// Indexes one batch of finalized blocks. Returns true while more finalized blocks remain.
    pub async fn step(&mut self) -> Result<bool> {
        let cursor = store::cursor(&self.db, &self.name).await?;
        let from = cursor.map_or(self.start_block, |c| c + 1);
        loop {
            // unbounded query: the response's archive height doubles as head, saving a request
            let page = self
                .src
                .fetch(from, &self.factory, &self.baskets, &self.markets)
                .await?;
            let Some((_, to)) = finality::range(
                cursor,
                self.start_block,
                page.archive_height,
                self.finality_blocks,
            ) else {
                return Ok(false);
            };
            let end = page.next_block.min(to);
            let mut rows = Vec::new();
            let mut grew = false;
            for l in page.logs.into_iter().filter(|l| l.block < end) {
                let Some(event) = decode::decode(&l.address, &l.topics, &l.data)
                    .with_context(|| format!("decode {} log {}", l.tx_hash, l.log_index))?
                else {
                    continue;
                };
                match &event {
                    Event::BasketCreated { basket, .. } => {
                        grew |= self.baskets.insert(basket.clone())
                    }
                    Event::MarketAllowed { market, .. } => {
                        grew |= self.markets.insert(market.clone())
                    }
                    _ => {}
                }
                rows.push(store::Row {
                    tx_hash: l.tx_hash,
                    log_index: l.log_index,
                    block: l.block as i64,
                    ts: l.ts,
                    event,
                });
            }
            // a new basket/market appeared: re-query the same range so its own logs are included
            if grew {
                continue;
            }
            store::write(&mut self.db, &self.name, &rows, end - 1).await?;
            return Ok(end < to);
        }
    }
}
