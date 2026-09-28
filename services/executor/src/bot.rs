//! seeder's market-maker account: BOT_PRIVATE_KEY signs locally, trades from its own AccountCore account.
use alloy::{
    consensus::{SignableTransaction, TxEip1559, TxEnvelope},
    eips::Encodable2718,
    primitives::{aliases::U40, Address, Bytes, TxKind, B256, U256},
    signers::{local::PrivateKeySigner, SignerSync},
    sol_types::SolCall,
};
use std::{collections::HashMap, time::Duration};
use tokio::sync::Mutex;

use crate::{
    config::Deployments,
    contracts::{self, *},
    gas,
    nonce::BotNonce,
    rpc::Rpc,
};

pub struct Bot {
    signer: PrivateKeySigner,
    pub address: Address,
    nonce: BotNonce,
    account_id: Mutex<u64>,
    // (market, orderId) → slot, learned from our own receipts; a miss falls back to a chain scan
    slots: Mutex<HashMap<(Address, u64), u8>>,
    http: reqwest::Client,
    gateway: String,
}

pub struct Placed {
    pub hash: Option<B256>,
    pub status: &'static str,
    pub order_ids: Vec<String>,
}

impl Bot {
    pub fn new(key: &str, gateway: String) -> Result<Self, String> {
        let signer: PrivateKeySigner = key.trim().parse().map_err(|_| "BOT_PRIVATE_KEY is not a secp256k1 key")?;
        Ok(Self { address: signer.address(), signer, nonce: BotNonce::default(), account_id: Mutex::new(0), slots: Mutex::default(), http: reqwest::Client::new(), gateway })
    }

    pub async fn send(&self, rpc: &Rpc, to: Address, value: U256, data: Vec<u8>) -> Result<B256, String> {
        let fees = gas::fees(rpc, self.address, to, value, &data).await?;
        let mut cached = self.nonce.0.lock().await;
        let nonce = match *cached {
            Some(n) => n,
            None => rpc.pending_nonce(self.address).await?,
        };
        let tx = TxEip1559 {
            chain_id: 10143,
            nonce,
            gas_limit: fees.gas_limit,
            max_fee_per_gas: fees.max_fee_per_gas,
            max_priority_fee_per_gas: fees.max_priority_fee_per_gas,
            to: TxKind::Call(to),
            value,
            access_list: Default::default(),
            input: Bytes::from(data),
        };
        let sig = self.signer.sign_hash_sync(&tx.signature_hash()).map_err(|e| e.to_string())?;
        let raw = TxEnvelope::from(tx.into_signed(sig)).encoded_2718();
        match rpc.send_raw(&raw).await {
            Ok(h) => {
                *cached = Some(nonce + 1);
                Ok(h)
            }
            Err(e) => {
                *cached = None;
                Err(e)
            }
        }
    }

    /// 0 until the first deposit registers the account.
    pub async fn account_id(&self, rpc: &Rpc, account_core: Address) -> Result<u64, String> {
        let mut id = self.account_id.lock().await;
        if *id == 0 {
            let out = rpc.eth_call(account_core, &userRegistryCall { user: self.address }.abi_encode()).await?;
            *id = userRegistryCall::abi_decode_returns(&out).map_err(|e| e.to_string())?.to::<u64>();
        }
        Ok(*id)
    }

    /// cache first; on a miss the gateway names the slot and getOrderId confirms it, so a reused slot is never cancelled.
    async fn slot_of(&self, rpc: &Rpc, market: Address, account_id: u64, order_ids: &[u64]) -> Result<Vec<u8>, String> {
        let known = self.slots.lock().await.clone();
        let mut out = vec![];
        let mut gateway: Option<HashMap<u64, u8>> = None;
        for id in order_ids {
            let slot = match known.get(&(market, *id)) {
                Some(s) => Some(*s),
                None => {
                    if gateway.is_none() {
                        gateway = Some(self.gateway_slots(market, account_id).await?);
                    }
                    gateway.as_ref().and_then(|g| g.get(id).copied())
                }
            };
            // gone ids (filled or already cancelled) have no slot and are skipped
            let Some(slot) = slot else { continue };
            let data = getOrderIdCall { userId: U40::from(account_id), slotIdx: slot }.abi_encode();
            if getOrderIdCall::abi_decode_returns(&rpc.eth_call(market, &data).await?).map_err(|e| e.to_string())? == *id {
                out.push(slot);
            }
        }
        out.sort_unstable();
        out.dedup();
        Ok(out)
    }

    async fn gateway_slots(&self, market: Address, account_id: u64) -> Result<HashMap<u64, u8>, String> {
        #[derive(serde::Deserialize)]
        #[serde(rename_all = "camelCase")]
        struct Row { order_id: String, market_address: Address, slot_idx: u8 }
        #[derive(serde::Deserialize)]
        struct Resp { data: Vec<Row> }
        let url = format!("{}/api/v1/users/{account_id}/orders", self.gateway);
        let r: Resp = self.http.get(&url).send().await.map_err(|e| e.to_string())?.json().await.map_err(|e| format!("{url}: {e}"))?;
        Ok(r.data.into_iter().filter(|o| o.market_address == market).filter_map(|o| Some((o.order_id.parse().ok()?, o.slot_idx))).collect())
    }

    pub async fn place(&self, rpc: &Rpc, dep: &Deployments, market: Address, cancels: &[u64], orders: Vec<NativeOrder>) -> Result<Placed, String> {
        let account_id = self.account_id(rpc, dep.kuru.account_core).await?;
        if account_id == 0 {
            return Err("bot has no AccountCore account: deposit first".into());
        }
        let slots = if cancels.is_empty() { vec![] } else { self.slot_of(rpc, market, account_id, cancels).await? };
        if slots.is_empty() && orders.is_empty() {
            return Ok(Placed { hash: None, status: "noop", order_ids: vec![] });
        }
        let hash = self.send(rpc, market, U256::ZERO, place_orders(account_id, orders, slots)).await?;
        let Some(receipt) = rpc.wait_receipt(&hash.to_string(), Duration::from_secs(15)).await else {
            return Ok(Placed { hash: Some(hash), status: "submitted", order_ids: vec![] });
        };
        let mut order_ids = vec![];
        let mut map = self.slots.lock().await;
        for u in contracts::book_updates(&receipt.logs, market, account_id) {
            map.retain(|(m, _), s| !(*m == market && *s == u.slot));
            if u.live {
                map.insert((market, u.order_id), u.slot);
                order_ids.push(u.order_id.to_string());
            }
        }
        Ok(Placed { hash: Some(hash), status: if receipt.ok() { "confirmed" } else { "failed" }, order_ids })
    }

    pub async fn deposit(&self, rpc: &Rpc, account_core: Address, token: Address, amount: U256) -> Result<B256, String> {
        if !token.is_zero() {
            let out = rpc.eth_call(token, &allowanceCall { owner: self.address, spender: account_core }.abi_encode()).await?;
            if allowanceCall::abi_decode_returns(&out).map_err(|e| e.to_string())? < amount {
                let h = self.send(rpc, token, U256::ZERO, approveCall { spender: account_core, amount }.abi_encode()).await?;
                match rpc.wait_receipt(&h.to_string(), Duration::from_secs(30)).await {
                    Some(r) if r.ok() => {}
                    _ => return Err(format!("approve {h} did not confirm")),
                }
            }
        }
        let value = if token.is_zero() { amount } else { U256::ZERO };
        self.send(rpc, account_core, value, depositCall { token, amount }.abi_encode()).await
    }

    /// (token, free, locked) for USDC and every market's base token.
    pub async fn balances(&self, rpc: &Rpc, dep: &Deployments) -> Result<Vec<(Address, U256, U256)>, String> {
        let mut tokens = vec![dep.kuru.usdc];
        for m in dep.kuru.markets.values() {
            let t = baseTokenCall::abi_decode_returns(&rpc.eth_call(*m, &baseTokenCall {}.abi_encode()).await?).map_err(|e| e.to_string())?;
            if !tokens.contains(&t) {
                tokens.push(t);
            }
        }
        let ac = dep.kuru.account_core;
        let mut out = vec![];
        for token in tokens {
            let free = rpc.eth_call(ac, &getBalanceCall { user: self.address, token }.abi_encode()).await?;
            let held = rpc.eth_call(ac, &getSpotReservedBalanceCall { user: self.address, token }.abi_encode()).await?;
            out.push((
                token,
                getBalanceCall::abi_decode_returns(&free).map_err(|e| e.to_string())?,
                getSpotReservedBalanceCall::abi_decode_returns(&held).map_err(|e| e.to_string())?,
            ));
        }
        Ok(out)
    }
}
