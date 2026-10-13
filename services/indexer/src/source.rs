use crate::config::Config;
use crate::decode::*;
use alloy_primitives::{Address, B256};
use anyhow::{Context, Result};
use hypersync_client::net_types::{BlockField, LogField, LogFilter, Query};
use hypersync_client::{format::Hex, Client, RateLimitResponse};
use std::collections::{BTreeSet, HashMap};
use std::time::Duration;

/// Fraction (1/n) of the per-window request limit kept in reserve for the history service, which shares the token.
const RESERVE_DIV: u64 = 5;

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

/// Wallets as 32-byte topic values.
fn topics(wallets: &BTreeSet<String>) -> Vec<String> {
    wallets
        .iter()
        .map(|w| format!("0x{:0>64}", &w[2..]))
        .collect()
}

/// Log filters for one query. Wallet-indexed events are filtered server-side; Kuru/Perpl/ActiveVault
/// index nothing, so they come unfiltered and are matched against wallets after decoding.
pub fn filters(cfg: &Config, wallets: &BTreeSet<String>) -> Result<Vec<LogFilter>> {
    let mut f = vec![
        LogFilter::all()
            .and_address(cfg.markets.iter().map(String::as_str))?
            .and_topic0(hexes(&KURU_TOPICS))?,
        LogFilter::all()
            .and_address([cfg.perpl.as_str()])?
            .and_topic0(hexes(&PERPL_TOPICS))?,
        LogFilter::all()
            .and_address([cfg.active_vault.as_str()])?
            .and_topic0(hexes(&ACTIVE_VAULT_TOPICS))?,
    ];
    // an empty topic list would match every wallet, so skip instead
    if wallets.is_empty() {
        return Ok(f);
    }
    let w = topics(wallets);
    let pools: Vec<&str> = cfg.pools.keys().map(String::as_str).collect();
    let vaults: Vec<&str> = cfg.vaults.keys().map(String::as_str).collect();
    f.push(
        LogFilter::all()
            .and_address(pools.clone())?
            .and_topic0([SUPPLY.to_string()])?
            .and_topic2(w.clone())?,
    );
    f.push(
        LogFilter::all()
            .and_address(pools.clone())?
            .and_topic0([AAVE_WITHDRAW.to_string()])?
            .and_topic2(w.clone())?,
    );
    f.push(
        LogFilter::all()
            .and_address(vaults.clone())?
            .and_topic0([DEPOSIT_4626.to_string()])?
            .and_topic2(w.clone())?,
    );
    f.push(
        LogFilter::all()
            .and_address(vaults.clone())?
            .and_topic0([WITHDRAW_4626.to_string()])?
            .and_topic3(w.clone())?,
    );
    if !cfg.treasury.is_empty() {
        f.push(
            LogFilter::all()
                .and_address([cfg.usdc.as_str()])?
                .and_topic0([TRANSFER.to_string()])?
                .and_topic1(w)?
                .and_topic2(topics(&[cfg.treasury.clone()].into()))?,
        );
    }
    Ok(f)
}

impl Source {
    pub fn new(url: &str, token: &str) -> Result<Self> {
        Ok(Source(Client::builder().url(url).api_token(token).build()?))
    }

    pub async fn height(&self) -> Result<u64> {
        self.0.get_height().await
    }

    pub async fn fetch(&self, from: u64, filters: Vec<LogFilter>) -> Result<Page> {
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
        let r = loop {
            match self.0.get_with_rate_limit(&q).await? {
                RateLimitResponse::Success {
                    response,
                    rate_limit,
                } => {
                    // remaining/limit are budget units; cost = units per request
                    let cost = rate_limit.cost.unwrap_or(1).max(1);
                    let reserve = rate_limit
                        .limit
                        .map_or(1, |l| (l / cost / RESERVE_DIV).max(1));
                    if rate_limit.remaining.is_some_and(|r| r / cost <= reserve) {
                        let secs = rate_limit.reset_secs.unwrap_or(5);
                        eprintln!("hypersync budget low ({rate_limit}), pausing {secs}s");
                        tokio::time::sleep(Duration::from_secs(secs)).await;
                    }
                    break response;
                }
                RateLimitResponse::RateLimited(info) => {
                    let secs = info.suggested_wait_secs().unwrap_or(5) + 1;
                    eprintln!("hypersync 429 ({info}), retry in {secs}s");
                    tokio::time::sleep(Duration::from_secs(secs)).await;
                }
            }
        };

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

#[cfg(test)]
mod tests {
    #[test]
    fn wallet_topic_is_left_padded() {
        let t = super::topics(&["0x52efc37fd2e56f61eb035978c9c4eed3864e4fca".to_string()].into());
        assert_eq!(
            t,
            ["0x00000000000000000000000052efc37fd2e56f61eb035978c9c4eed3864e4fca"]
        );
    }
}
