use crate::decode::{TOPICS_BASKET, TOPICS_FACTORY, TOPIC_TRADES};
use alloy_primitives::{Address, B256};
use anyhow::{Context, Result};
use hypersync_client::net_types::{BlockField, LogField, LogFilter, Query};
use hypersync_client::{format::Hex, Client};
use std::collections::{BTreeSet, HashMap};

pub struct RawLog {
    pub block: u64,
    pub ts: i64,
    pub tx_hash: String,
    pub log_index: i32,
    pub address: Address,
    pub topics: Vec<B256>,
    pub data: Vec<u8>,
}

pub struct Page {
    pub logs: Vec<RawLog>,
    /// Everything below this block was covered by the response.
    pub next_block: u64,
    pub archive_height: u64,
}

pub struct Source(Client);

fn hexes(t: &[B256]) -> Vec<String> {
    t.iter().map(|t| t.to_string()).collect()
}

impl Source {
    pub fn new(url: &str, token: &str) -> Result<Self> {
        Ok(Source(Client::builder().url(url).api_token(token).build()?))
    }

    pub async fn fetch(
        &self,
        from: u64,
        factory: &str,
        baskets: &BTreeSet<String>,
        markets: &BTreeSet<String>,
    ) -> Result<Page> {
        let mut filters = vec![LogFilter::all()
            .and_address([factory])?
            .and_topic0(hexes(&TOPICS_FACTORY))?];
        // an empty address list would match every contract, so skip the filter instead
        if !baskets.is_empty() {
            filters.push(
                LogFilter::all()
                    .and_address(baskets.iter().map(String::as_str))?
                    .and_topic0(hexes(&TOPICS_BASKET))?,
            );
        }
        if !markets.is_empty() {
            filters.push(
                LogFilter::all()
                    .and_address(markets.iter().map(String::as_str))?
                    .and_topic0(hexes(&[TOPIC_TRADES]))?,
            );
        }
        let mut q = Query::new()
            .from_block(from)
            .select_log_fields([
                LogField::BlockNumber,
                LogField::LogIndex,
                LogField::TransactionHash,
                LogField::Address,
                LogField::Topic0,
                LogField::Topic1,
                LogField::Topic2,
                LogField::Topic3,
                LogField::Data,
            ])
            .select_block_fields([BlockField::Number, BlockField::Timestamp]);
        q.logs = filters.into_iter().map(Into::into).collect();
        let r = self.0.get(&q).await?;

        let mut ts = HashMap::new();
        for b in r.data.blocks.iter().flatten() {
            let t: &[u8] = b.timestamp.as_ref().context("block timestamp")?;
            ts.insert(
                b.number.context("block number")?,
                t.iter().fold(0i64, |a, x| a << 8 | *x as i64),
            );
        }
        let mut logs = Vec::new();
        for l in r.data.logs.into_iter().flatten() {
            let block: u64 = l.block_number.context("log block")?.into();
            logs.push(RawLog {
                block,
                ts: *ts
                    .get(&block)
                    .with_context(|| format!("no header for block {block}"))?,
                tx_hash: l.transaction_hash.context("tx hash")?.encode_hex(),
                log_index: u64::from(l.log_index.context("log index")?) as i32,
                address: Address::from_slice(l.address.context("address")?.as_ref()),
                topics: l
                    .topics
                    .iter()
                    .flatten()
                    .map(|t| B256::from_slice(t.as_ref()))
                    .collect(),
                data: l.data.map(|d| d.to_vec()).unwrap_or_default(),
            });
        }
        logs.sort_by_key(|l| (l.block, l.log_index));
        Ok(Page {
            logs,
            next_block: r.next_block,
            archive_height: r.archive_height.context("archive height")?,
        })
    }
}
