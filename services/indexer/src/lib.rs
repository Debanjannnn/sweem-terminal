pub mod config;
pub mod decode;
pub mod finality;
pub mod health;
pub mod source;
pub mod store;

use anyhow::{Context, Result};
use decode::Event;
use std::collections::{BTreeSet, HashMap};
use store::Rec;

pub struct Indexer {
    name: String,
    cfg: config::Config,
    src: source::Source,
    db: tokio_postgres::Client,
}

/// Records for our wallets from one decoded event. `perpl` (account id -> wallet) grows on AccountCreated.
pub fn recs(e: Event, wallets: &BTreeSet<String>, perpl: &mut HashMap<String, String>) -> Vec<Rec> {
    let side = |buy: bool| if buy { "buy" } else { "sell" };
    match e {
        // isBuy is the taker's direction (verified: isBuy=false fills walk bids down)
        Event::Trade {
            order_id,
            maker,
            taker,
            is_buy,
            price,
            updated_size,
            filled_size,
        } => [("maker", maker, !is_buy), ("taker", taker, is_buy)]
            .into_iter()
            .filter(|(_, w, _)| wallets.contains(w))
            .map(|(role, wallet, buy)| Rec::Trade {
                role,
                wallet,
                side: side(buy),
                order_id,
                price: price.clone(),
                size: filled_size.clone(),
                updated_size: updated_size.clone(),
            })
            .collect(),
        Event::OrderCreated {
            order_id,
            owner,
            is_buy,
            price,
            size,
        } if wallets.contains(&owner) => {
            vec![Rec::Order {
                order_id,
                wallet: owner,
                kind: "created",
                side: Some(side(is_buy)),
                price: Some(price),
                size: Some(size),
            }]
        }
        Event::OrdersCanceled { owner, ids } if wallets.contains(&owner) => ids
            .into_iter()
            .map(|order_id| Rec::Order {
                order_id,
                wallet: owner.clone(),
                kind: "canceled",
                side: None,
                price: None,
                size: None,
            })
            .collect(),
        Event::PerplAccount { wallet, id } if wallets.contains(&wallet) => {
            perpl.insert(id.clone(), wallet.clone());
            vec![Rec::PerplAccount { id, wallet }]
        }
        Event::Venue(mut v) => {
            if v.venue == "perpl" {
                match perpl.get(&v.account) {
                    Some(w) => v.account = w.clone(),
                    None => return vec![],
                }
            }
            if wallets.contains(&v.account) {
                vec![Rec::Venue(v)]
            } else {
                vec![]
            }
        }
        _ => vec![],
    }
}

impl Indexer {
    /// `name` keys the row in `indexer_cursor`.
    pub async fn new(cfg: config::Config, name: &str) -> Result<Self> {
        Ok(Indexer {
            name: name.into(),
            src: source::Source::new(&cfg.hypersync_url, &cfg.envio_token)?,
            db: store::connect(&cfg.database_url).await?,
            cfg,
        })
    }

    /// Indexes one batch of finalized blocks. Returns true while more finalized blocks remain.
    pub async fn step(&mut self) -> Result<bool> {
        let wallets = store::sync_wallets(&mut self.db, &self.name).await?;
        let mut perpl = store::perpl_accounts(&self.db).await?;
        let cursor = store::cursor(&self.db, &self.name).await?;
        let from = cursor.map_or(self.cfg.start_block, |c| c + 1);
        // unbounded query: the response's archive height doubles as head, saving a request
        let page = self
            .src
            .fetch(from, source::filters(&self.cfg, &wallets)?)
            .await?;
        let Some((_, to)) = finality::range(
            cursor,
            self.cfg.start_block,
            page.archive_height,
            self.cfg.finality_blocks,
        ) else {
            return Ok(false);
        };
        let end = page.next_block.min(to);
        let mut rows = Vec::new();
        for l in page.logs.into_iter().filter(|l| l.block < end) {
            let Some(event) = decode::decode(&self.cfg, &l.address, &l.topics, &l.data)
                .with_context(|| format!("decode {} log {}", l.tx_hash, l.log_index))?
            else {
                continue;
            };
            for rec in recs(event, &wallets, &mut perpl) {
                rows.push(store::Row {
                    tx_hash: l.tx_hash.clone(),
                    log_index: l.log_index,
                    block: l.block as i64,
                    ts: l.ts,
                    contract: decode::addr(l.address),
                    rec,
                });
            }
        }
        store::write(&mut self.db, &self.name, &rows, end - 1).await?;
        Ok(end < to)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use decode::Venue;

    #[test]
    fn recs_match_wallets() {
        let (a, b) = ("0xaa".to_string(), "0xbb".to_string());
        let wallets: BTreeSet<String> = [a.clone(), b.clone()].into();
        let mut perpl = HashMap::new();
        let trade = Event::Trade {
            order_id: 7,
            maker: a.clone(),
            taker: b.clone(),
            is_buy: true,
            price: "1".into(),
            updated_size: "0".into(),
            filled_size: "2".into(),
        };
        let sides: Vec<_> = recs(trade, &wallets, &mut perpl)
            .into_iter()
            .map(|r| match r {
                Rec::Trade { role, side, .. } => (role, side),
                _ => unreachable!(),
            })
            .collect();
        assert_eq!(sides, [("maker", "sell"), ("taker", "buy")]);

        let perp = |acct: &str| {
            Event::Venue(Venue {
                venue: "perpl".into(),
                kind: "collateral_deposit",
                account: acct.into(),
                asset: None,
                amount: Some("1".into()),
                shares: None,
                extra: None,
            })
        };
        assert!(recs(perp("9"), &wallets, &mut perpl).is_empty());
        assert_eq!(
            recs(
                Event::PerplAccount {
                    wallet: "0xcc".into(),
                    id: "8".into()
                },
                &wallets,
                &mut perpl
            ),
            []
        );
        recs(
            Event::PerplAccount {
                wallet: a.clone(),
                id: "9".into(),
            },
            &wallets,
            &mut perpl,
        );
        match &recs(perp("9"), &wallets, &mut perpl)[..] {
            [Rec::Venue(v)] => assert_eq!(v.account, a),
            r => panic!("{r:?}"),
        }
    }
}
