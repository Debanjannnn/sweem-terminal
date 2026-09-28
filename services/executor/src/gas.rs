use alloy::primitives::{Address, U256};

use crate::rpc::Rpc;

/// monad bills the whole gas limit, so the limit is always explicit and tight.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Fees {
    pub gas_limit: u64,
    pub max_fee_per_gas: u128,
    pub max_priority_fee_per_gas: u128,
}

pub fn buffered(estimate: u64) -> u64 {
    estimate * 6 / 5
}

/// estimate×1.2. an estimate that reverts is the tx's own revert, surfaced before signing.
pub async fn fees(rpc: &Rpc, from: Address, to: Address, value: U256, data: &[u8]) -> Result<Fees, String> {
    let est = rpc.estimate_gas(from, to, value, data).await?;
    let base = rpc.base_fee().await?;
    let tip = rpc.priority_fee().await.unwrap_or(1_000_000_000);
    Ok(Fees { gas_limit: buffered(est), max_fee_per_gas: base * 2 + tip, max_priority_fee_per_gas: tip })
}

#[cfg(test)]
mod tests {
    #[test]
    fn buffer_is_twenty_percent() {
        assert_eq!(super::buffered(100_000), 120_000);
    }
}
