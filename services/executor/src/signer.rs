use alloy::{
    consensus::{SignableTransaction, TxEip1559, TxEnvelope},
    eips::Encodable2718,
    primitives::{Address, Bytes, TxKind, U256},
    signers::{local::PrivateKeySigner, SignerSync},
    sol_types::SolCall,
};
use std::{sync::Arc, time::Duration};

use crate::{config::CHAIN_ID, contracts::IERC20, gas, privy::Privy, rpc::Rpc, steps::Call};

/// Privy = the user's delegated wallet (production). Local = a raw key, for the mainnet-fork tests only.
pub enum Signer {
    Privy(Arc<Privy>, String),
    Local(PrivateKeySigner),
}

impl Signer {
    pub async fn send(&self, rpc: &Rpc, from: Address, to: Address, value: U256, data: &[u8]) -> Result<String, String> {
        let fees = gas::fees(rpc, from, to, value, data).await?;
        match self {
            Signer::Privy(p, wallet_id) => p.send_transaction(wallet_id, to, value, data, &fees).await,
            Signer::Local(key) => {
                let tx = TxEip1559 {
                    chain_id: CHAIN_ID,
                    nonce: rpc.pending_nonce(from).await?,
                    gas_limit: fees.gas_limit,
                    max_fee_per_gas: fees.max_fee_per_gas,
                    max_priority_fee_per_gas: fees.max_priority_fee_per_gas,
                    to: TxKind::Call(to),
                    value,
                    access_list: Default::default(),
                    input: Bytes::copy_from_slice(data),
                };
                let sig = key.sign_hash_sync(&tx.signature_hash()).map_err(|e| e.to_string())?;
                rpc.send_raw(&TxEnvelope::from(tx.into_signed(sig)).encoded_2718()).await.map(|h| h.to_string())
            }
        }
    }

    async fn send_confirmed(&self, rpc: &Rpc, from: Address, to: Address, value: U256, data: &[u8]) -> Result<String, String> {
        let h = self.send(rpc, from, to, value, data).await?;
        match rpc.wait_receipt(&h, Duration::from_secs(60)).await {
            Some(r) if r.ok() => Ok(h),
            Some(_) => Err(format!("{h} reverted")),
            None => Err(format!("{h} no receipt in 60s")),
        }
    }

    /// approve exactly `amount` when allowance is short, then the call; both must confirm. returns the call's hash.
    pub async fn perform(&self, rpc: &Rpc, wallet: Address, c: &Call) -> Result<String, String> {
        if let (true, Some((token, amount))) = (c.approve, c.spend) {
            let out = rpc.eth_call(token, &IERC20::allowanceCall { owner: wallet, spender: c.to }.abi_encode()).await?;
            if IERC20::allowanceCall::abi_decode_returns(&out).map_err(|e| e.to_string())? < amount {
                let approve = IERC20::approveCall { spender: c.to, amount }.abi_encode();
                self.send_confirmed(rpc, wallet, token, U256::ZERO, &approve).await.map_err(|e| format!("approve: {e}"))?;
            }
        }
        self.send_confirmed(rpc, wallet, c.to, c.value, &c.data).await
    }
}
