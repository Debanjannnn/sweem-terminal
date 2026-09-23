//! monad charges the FULL gas limit up front, not the gas used. an unset limit
//! means privy's default is billed in full and even a funded wallet fails with
//! "Signer had insufficient balance". every transaction ships an explicit limit.
use alloy_primitives::{Address, U256};

use crate::rpc::Rpc;

pub const APPROVE: u64 = 80_000;
pub const SWAP: u64 = 900_000;
pub const DEPOSIT: u64 = 250_000;
pub const BATCH: u64 = 1_200_000;
pub const AUTHORIZE: u64 = 150_000;

const GWEI: u128 = 1_000_000_000;
// only used when the node will not answer; monad's base fee sits near 100 gwei
const FALLBACK_BASE_FEE: u128 = 100 * GWEI;
const FALLBACK_TIP: u128 = GWEI;
const BUFFER_NUM: u64 = 125;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Fees {
    pub gas_limit: u64,
    pub max_fee_per_gas: u128,
    pub max_priority_fee_per_gas: u128,
}

impl Fees {
    /// what the sender is debited at submission: the whole limit, not an estimate.
    pub fn max_cost(&self) -> U256 {
        U256::from(self.gas_limit as u128 * self.max_fee_per_gas)
    }
}

/// estimate + 25%, falling back to `fallback` when the node will not estimate.
/// the estimate is never trusted below the fallback floor on a revert path.
pub async fn limit(rpc: &Rpc, from: Address, to: Address, value: U256, data: &[u8], fallback: u64) -> u64 {
    match rpc.estimate_gas(from, to, value, data).await {
        Some(est) => (est * BUFFER_NUM / 100).max(21_000),
        None => fallback,
    }
}

pub async fn fees(rpc: &Rpc, gas_limit: u64) -> Fees {
    let base = rpc.base_fee().await.unwrap_or(FALLBACK_BASE_FEE);
    let tip = rpc.priority_fee().await.unwrap_or(FALLBACK_TIP);
    Fees {
        gas_limit,
        max_priority_fee_per_gas: tip,
        max_fee_per_gas: base * 2 + tip,
    }
}

/// fails before privy does, with a number the caller can act on.
pub async fn affordable(rpc: &Rpc, from: Address, fees: &Fees, value: U256) -> Result<(), String> {
    let need = fees.max_cost() + value;
    match rpc.balance(from).await {
        Some(have) if have < need => Err(format!(
            "insufficient balance: have {have} wei, need {need} wei (gas limit {} is charged in full on monad)",
            fees.gas_limit
        )),
        _ => Ok(()),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn cost_bills_the_whole_limit() {
        let f = Fees { gas_limit: 21_000, max_fee_per_gas: 100 * GWEI, max_priority_fee_per_gas: GWEI };
        assert_eq!(f.max_cost(), U256::from(21_000u128 * 100 * GWEI));
        // a 21k transfer reserves 2.1 MON at 100 gwei: the reason the limit must be tight
        assert_eq!(f.max_cost(), U256::from(2_100_000_000_000_000u128));
    }

    #[test]
    fn a_bigger_limit_costs_more_even_for_the_same_call() {
        let cheap = Fees { gas_limit: 21_000, max_fee_per_gas: GWEI, max_priority_fee_per_gas: 0 };
        let default = Fees { gas_limit: 30_000_000, max_fee_per_gas: GWEI, max_priority_fee_per_gas: 0 };
        assert!(default.max_cost() > cheap.max_cost() * U256::from(1000));
    }
}
