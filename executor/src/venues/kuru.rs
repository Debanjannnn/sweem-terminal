use alloy_primitives::{Address, U256};
use alloy_sol_types::{sol, SolCall};

use crate::{
    rpc::Rpc,
    venues::{Call, Market, Side},
};

// the router is an ERC-1967 proxy: calls go to the proxy, the abi below was
// read off the implementation at slot
// 0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc.
// orderbook write selectors are unverifiable today, so market orders only.
sol! {
    function anyToAnySwap(
        address[] marketAddresses,
        bool[] isBuy,
        bool[] nativeSend,
        address debitToken,
        address creditToken,
        uint256 amount,
        uint256 minAmountOut
    ) external payable returns (uint256);

    function approve(address spender, uint256 amount) external returns (bool);
    function allowance(address owner, address spender) external view returns (uint256);
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Swap {
    pub debit: Address,
    pub credit: Address,
    pub is_buy: bool,
    /// the debited token is native MON
    pub native: bool,
}

/// buy spends quote for base, sell spends base for quote.
pub fn plan(m: &Market, side: Side) -> Swap {
    let (debit, credit) = match side {
        Side::Buy => (m.quote_token, m.base_token),
        Side::Sell => (m.base_token, m.quote_token),
    };
    Swap { debit, credit, is_buy: side == Side::Buy, native: debit.is_zero() }
}

pub fn swap_call(router: Address, m: &Market, s: &Swap, amount: U256, min_out: U256) -> Call {
    Call {
        to: router,
        value: if s.native { amount } else { U256::ZERO },
        data: anyToAnySwapCall {
            marketAddresses: vec![m.market_address],
            isBuy: vec![s.is_buy],
            nativeSend: vec![s.native],
            debitToken: s.debit,
            creditToken: s.credit,
            amount,
            minAmountOut: min_out,
        }
        .abi_encode()
        .into(),
    }
}

/// simulated output, sized with no protection. only ever used to derive one.
pub async fn quote(rpc: &Rpc, router: Address, user: Address, m: &Market, s: &Swap, amount: U256) -> Result<U256, String> {
    let c = swap_call(router, m, s, amount, U256::ZERO);
    let out = rpc.eth_call(user, c.to, c.value, &c.data).await?;
    match (out.len() >= 32).then(|| U256::from_be_slice(&out[..32])) {
        Some(o) if !o.is_zero() => Ok(o),
        _ => Err(format!("router returned no output for {}", m.id)),
    }
}

pub fn min_out(quoted: U256, slippage_bps: u32) -> U256 {
    quoted * U256::from(10_000 - slippage_bps as u64) / U256::from(10_000)
}

pub fn approve_call(token: Address, spender: Address, amount: U256) -> Call {
    Call {
        to: token,
        value: U256::ZERO,
        data: approveCall { spender, amount }.abi_encode().into(),
    }
}

/// an unreadable allowance approves anyway: a redundant approval costs gas, a
/// missing one fails the swap behind it.
pub async fn approve_if_needed(
    rpc: &Rpc,
    token: Address,
    spender: Address,
    owner: Address,
    amount: U256,
) -> Option<Call> {
    let data = allowanceCall { owner, spender }.abi_encode();
    match rpc.eth_call(owner, token, U256::ZERO, &data).await {
        Ok(out) if out.len() >= 32 && U256::from_be_slice(&out[..32]) >= amount => None,
        _ => Some(approve_call(token, spender, amount)),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::venues::test_registry;

    const ROUTER: Address = alloy_primitives::address!("7EFbE105Ca7415dE98F96622173458ac1c054630");

    fn mon() -> Market {
        test_registry().get("MON-USDC").unwrap().clone()
    }

    fn weth() -> Market {
        test_registry().get("WETH-USDC").unwrap().clone()
    }

    #[test]
    fn buy_debits_quote_and_sell_debits_base() {
        let m = weth();
        let b = plan(&m, Side::Buy);
        assert_eq!((b.debit, b.credit, b.is_buy), (m.quote_token, m.base_token, true));
        let s = plan(&m, Side::Sell);
        assert_eq!((s.debit, s.credit, s.is_buy), (m.base_token, m.quote_token, false));
        assert!(!b.native && !s.native, "an erc20 pair is never a native send");
    }

    #[test]
    fn selling_native_mon_carries_value_buying_it_does_not() {
        let m = mon();
        let sell = plan(&m, Side::Sell);
        assert!(sell.native, "MON base token is address(0)");
        let amount = U256::from(1_000u64);
        assert_eq!(swap_call(ROUTER, &m, &sell, amount, U256::ZERO).value, amount);

        let buy = plan(&m, Side::Buy);
        assert!(!buy.native);
        assert_eq!(swap_call(ROUTER, &m, &buy, amount, U256::ZERO).value, U256::ZERO);
    }

    #[test]
    fn calldata_round_trips_and_targets_the_router() {
        let m = weth();
        let s = plan(&m, Side::Buy);
        let c = swap_call(ROUTER, &m, &s, U256::from(5u64), U256::from(4u64));
        assert_eq!(c.to, ROUTER, "to is the router, never a request-supplied address");
        assert_eq!(c.data[..4], anyToAnySwapCall::SELECTOR);

        let d = anyToAnySwapCall::abi_decode(&c.data).unwrap();
        assert_eq!(d.marketAddresses, vec![m.market_address]);
        assert_eq!(d.isBuy, vec![true]);
        assert_eq!(d.debitToken, m.quote_token);
        assert_eq!(d.creditToken, m.base_token);
        assert_eq!(d.minAmountOut, U256::from(4u64));
    }

    #[test]
    fn min_out_applies_slippage_downward() {
        let q = U256::from(1_000_000u64);
        assert_eq!(min_out(q, 50), U256::from(995_000u64));
        assert_eq!(min_out(q, 300), U256::from(970_000u64));
        assert!(min_out(q, 10) < q, "protection must always bite");
        assert_eq!(min_out(U256::ZERO, 50), U256::ZERO);
    }

    #[test]
    fn approve_targets_the_token_and_spends_to_the_router() {
        let m = weth();
        let c = approve_call(m.base_token, ROUTER, U256::from(7u64));
        assert_eq!(c.to, m.base_token);
        assert_eq!(c.value, U256::ZERO);
        let d = approveCall::abi_decode(&c.data).unwrap();
        assert_eq!(d.spender, ROUTER);
        assert_eq!(d.amount, U256::from(7u64));
    }
}
