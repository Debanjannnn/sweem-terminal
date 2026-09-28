//! kuru spot v2. the orderbook proxy is the only trading entrypoint: the v2
//! SpotRouter is a registry with no swap function, and AccountCore holds the funds.
use alloy_primitives::{
    aliases::{U40, U96},
    Address, U256,
};
use alloy_sol_types::{sol, SolCall};

use crate::{
    rpc::Rpc,
    venues::{Call, Market, Side},
};

sol! {
    struct NativeOrder {
        uint8 side;
        uint96 quantity;
        uint32 price;
        uint8 tif;
        uint8 executionInstruction;
        uint32 minSizeAfterBlock;
    }

    function batch(uint40 userId, NativeOrder[] orders, uint8[] cancelSlotIdxs, bytes32 clientOrderId) external;
    function swap(uint40 userId, bool isBuy, uint128 amountIn, uint128 minAmountOut, uint64 deadline)
        external returns (uint128 amountInUsed, uint128 amountOut);
    function estimateSwap(uint40 userId, bool isBuy, uint128 amountIn)
        external view returns (uint128 amountInUsed, uint128 amountOut);
    function getOrderId(uint40 userId, uint8 slotIdx) external view returns (uint64);
    function bestBidAsk() external view returns (uint32 bid, uint32 ask);
}

pub const MAKER_SLOTS: u8 = 62;
pub const PPS: u64 = 10_000_000;
pub const SIDE_BUY: u8 = 0;
pub const SIDE_SELL: u8 = 1;
pub const TIF_GTC: u8 = 0;
pub const EXEC_NONE: u8 = 0;
pub const EXEC_POST_ONLY: u8 = 1;
/// bestBidAsk sentinels: neither is a tradeable price
const NO_BID: u32 = u32::MAX;
const NO_ASK: u32 = 0;

pub fn tif(s: &str) -> Option<u8> {
    match s.trim().to_ascii_lowercase().as_str() {
        "" | "gtc" => Some(TIF_GTC),
        "ioc" => Some(1),
        "fok" => Some(2),
        _ => None,
    }
}

/// book quantity -> what the token contract actually moves.
pub fn raw_base_atoms(m: &Market, quantity: U256) -> U256 {
    quantity * m.base_size_multiplier
}

/// the value min/max notional are checked against. floors twice, exactly as the
/// venue does; float math disagrees at the boundary.
pub fn quote_notional(m: &Market, price: U256, quantity: U256) -> U256 {
    let base = price * quantity / m.size_precision;
    base * U256::from(10u64).pow(U256::from(m.quote_decimals)) / m.price_precision
}

/// maker fees floor per record.
pub fn fee(amount: U256, pps: U256) -> U256 {
    amount * pps / U256::from(PPS)
}

/// taker fees ceil, over the whole match group. one record is the group here.
pub fn fee_ceil(amount: U256, pps: U256) -> U256 {
    let d = U256::from(PPS);
    (amount * pps).div_ceil(d)
}

/// a limit order the venue will accept, or the reason it would not.
pub fn check_order(m: &Market, price: U256, quantity: U256) -> Result<(), String> {
    if price.is_zero() || quantity.is_zero() {
        return Err("price and quantity must be greater than zero".into());
    }
    if price > U256::from(u32::MAX) {
        return Err(format!("price {price} exceeds uint32"));
    }
    if quantity > (U256::from(1u64) << 96) - U256::from(1u64) {
        return Err(format!("quantity {quantity} exceeds uint96"));
    }
    if price % m.tick_size != U256::ZERO {
        return Err(format!("price {price} is not a multiple of tick size {}", m.tick_size));
    }
    let q = quote_notional(m, price, quantity);
    if q < m.min_quote_notional {
        return Err(format!("notional {q} is below the market minimum {}", m.min_quote_notional));
    }
    if q > m.max_quote_notional {
        return Err(format!("notional {q} is above the market maximum {}", m.max_quote_notional));
    }
    Ok(())
}

/// a crossing post-only order is silently skipped on chain, not reverted: refuse
/// it here so a successful tx never has to mean "maybe".
pub fn crosses(side: Side, price: U256, bid: u32, ask: u32) -> bool {
    match side {
        Side::Buy => ask != NO_ASK && price >= U256::from(ask),
        Side::Sell => bid != NO_BID && price <= U256::from(bid),
    }
}

// checked by check_order first: the high limbs must already be zero
fn order(side: Side, price: U256, quantity: U256, tif: u8, post_only: bool) -> Result<NativeOrder, String> {
    if quantity.as_limbs()[2] != 0 || quantity.as_limbs()[3] != 0 {
        return Err("quantity exceeds uint96".into());
    }
    Ok(NativeOrder {
        side: if side.is_buy() { SIDE_BUY } else { SIDE_SELL },
        quantity: U96::from_be_slice(&quantity.to_be_bytes::<32>()[20..]),
        price: price.to::<u32>(),
        tif,
        executionInstruction: if post_only { EXEC_POST_ONLY } else { EXEC_NONE },
        minSizeAfterBlock: 0,
    })
}

fn user(user_id: u64) -> U40 {
    U40::try_from(user_id).unwrap_or(U40::ZERO)
}

/// overload 0x6947f147: the clientOrderId variant. it is the only handle that ties
/// our intent_key to the BookUpdatesPacked / TradesPacked logs the order produces,
/// and we never use builder fees, so the builder overloads would carry a dead tuple.
pub fn batch_call(
    m: &Market,
    user_id: u64,
    orders: Vec<NativeOrder>,
    cancel_slots: Vec<u8>,
    client_order_id: [u8; 32],
) -> Call {
    Call {
        to: m.market_address,
        value: U256::ZERO,
        data: batchCall {
            userId: user(user_id),
            orders,
            cancelSlotIdxs: cancel_slots,
            clientOrderId: client_order_id.into(),
        }
        .abi_encode()
        .into(),
    }
}

// one argument per field of the on-chain order; a struct would only rename them
#[allow(clippy::too_many_arguments)]
pub fn limit_call(
    m: &Market,
    user_id: u64,
    side: Side,
    price: U256,
    quantity: U256,
    tif_code: u8,
    post_only: bool,
    client_order_id: [u8; 32],
) -> Result<Call, String> {
    check_order(m, price, quantity)?;
    if post_only && tif_code != TIF_GTC {
        return Err("post_only requires tif gtc".into());
    }
    let o = order(side, price, quantity, tif_code, post_only)?;
    Ok(batch_call(m, user_id, vec![o], vec![], client_order_id))
}

pub fn cancel_call(m: &Market, user_id: u64, slot_idx: u8, client_order_id: [u8; 32]) -> Result<Call, String> {
    if slot_idx >= MAKER_SLOTS {
        return Err(format!("slot {slot_idx} is outside 0..{}", MAKER_SLOTS - 1));
    }
    Ok(batch_call(m, user_id, vec![], vec![slot_idx], client_order_id))
}

/// exact-input taker leg. amounts are raw token atoms of the input/output token,
/// never book units.
pub fn swap_call(m: &Market, user_id: u64, side: Side, amount_in: U256, min_out: U256, deadline: u64) -> Result<Call, String> {
    Ok(Call {
        to: m.market_address,
        value: U256::ZERO,
        data: swapCall {
            userId: user(user_id),
            isBuy: side.is_buy(),
            amountIn: u128::try_from(amount_in).map_err(|_| "amount exceeds uint128".to_string())?,
            minAmountOut: u128::try_from(min_out).map_err(|_| "min_out exceeds uint128".to_string())?,
            deadline,
        }
        .abi_encode()
        .into(),
    })
}

/// simulated output, sized with no protection. only ever used to derive one.
pub async fn estimate_swap(rpc: &Rpc, from: Address, m: &Market, user_id: u64, side: Side, amount_in: U256) -> Result<U256, String> {
    let data = estimateSwapCall {
        userId: user(user_id),
        isBuy: side.is_buy(),
        amountIn: u128::try_from(amount_in).map_err(|_| "amount exceeds uint128".to_string())?,
    }
    .abi_encode();
    let out = rpc.eth_call(from, m.market_address, U256::ZERO, &data).await?;
    let d = estimateSwapCall::abi_decode_returns(&out).map_err(|e| e.to_string())?;
    match U256::from(d.amountOut) {
        o if o.is_zero() => Err(format!("{} has no liquidity for this size", m.id)),
        o => Ok(o),
    }
}

pub async fn order_id_at(rpc: &Rpc, from: Address, m: &Market, user_id: u64, slot: u8) -> Result<u64, String> {
    let data = getOrderIdCall { userId: user(user_id), slotIdx: slot }.abi_encode();
    let out = rpc.eth_call(from, m.market_address, U256::ZERO, &data).await?;
    getOrderIdCall::abi_decode_returns(&out).map_err(|e| e.to_string())
}

/// where a resting GTC remainder will land. slots are reused, so this is only
/// meaningful in the same breath as the placement it is read for.
pub async fn lowest_free_slot(rpc: &Rpc, from: Address, m: &Market, user_id: u64) -> Option<u8> {
    for slot in 0..MAKER_SLOTS {
        if order_id_at(rpc, from, m, user_id, slot).await.ok()? == 0 {
            return Some(slot);
        }
    }
    None
}

pub async fn best_bid_ask(rpc: &Rpc, from: Address, m: &Market) -> Result<(u32, u32), String> {
    let out = rpc.eth_call(from, m.market_address, U256::ZERO, &bestBidAskCall {}.abi_encode()).await?;
    let d = bestBidAskCall::abi_decode_returns(&out).map_err(|e| e.to_string())?;
    Ok((d.bid, d.ask))
}

pub fn min_out(quoted: U256, slippage_bps: u32) -> U256 {
    quoted * U256::from(10_000 - slippage_bps as u64) / U256::from(10_000)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::venues::test_registry;

    fn m(id: &str) -> Market {
        test_registry().get(id).unwrap().clone()
    }

    fn n(v: u128) -> U256 {
        U256::from(v)
    }

    /// spec worked example, WETH/USDC: buy 0.5 WETH @ 2750.00.
    #[test]
    fn weth_scales_through_every_unit() {
        let w = m("WETH-USDC");
        let (price, qty) = (n(275_000), n(5_000_000_000));
        assert_eq!(raw_base_atoms(&w, qty), n(500_000_000_000_000_000), "0.5 WETH at 18dp");
        assert_eq!(quote_notional(&w, price, qty), n(1_375_000_000), "1375.00 USDC");
        assert_eq!(fee(n(1_375_000_000), w.taker_fee_pps), n(962_500));
        assert_eq!(fee(n(1_375_000_000), w.maker_fee_pps), n(550_000));
        // pps denominator is 10_000_000: taker 7000 = 0.07%, maker 4000 = 0.04%
        assert_eq!(PPS, 10_000_000);
        assert_eq!(fee(n(1), w.taker_fee_pps), U256::ZERO, "maker floors");
        assert_eq!(fee_ceil(n(1), w.taker_fee_pps), n(1), "taker ceils");
        assert!(check_order(&w, price, qty).is_ok());
    }

    /// cbBTC multiplier is 1: book quantity already is the raw atom count.
    #[test]
    fn cbbtc_scales_through_every_unit() {
        let c = m("CBBTC-USDC");
        let (price, qty) = (n(8_600_000), n(100_000));
        assert_eq!(raw_base_atoms(&c, qty), n(100_000));
        assert_eq!(quote_notional(&c, price, qty), n(86_000_000));
        assert_eq!(fee(n(86_000_000), c.taker_fee_pps), n(60_200));
        assert!(check_order(&c, price, qty).is_ok());
    }

    #[test]
    fn xaut0_scales_through_every_unit() {
        let x = m("XAUT0-USDC");
        let (price, qty) = (n(432_500), n(10_000));
        assert_eq!(raw_base_atoms(&x, qty), n(10_000));
        assert_eq!(quote_notional(&x, price, qty), n(43_250_000));
        assert!(check_order(&x, price, qty).is_ok());
    }

    /// spec worked example: 100 MON @ 0.025 is 2.50 USDC and the venue rejects it.
    #[test]
    fn mon_below_min_notional_is_refused_before_it_costs_gas() {
        let mon = m("MON-USDC");
        let (price, qty) = (n(25_000), n(10_000_000_000));
        assert_eq!(raw_base_atoms(&mon, qty), n(100_000_000_000_000_000_000), "100 MON at 18dp");
        assert_eq!(quote_notional(&mon, price, qty), n(2_500_000), "2.50 USDC");
        let err = check_order(&mon, price, qty).unwrap_err();
        assert!(err.contains("below the market minimum"), "{err}");
        // 400 MON at the same price clears the 10 USDC floor
        assert!(check_order(&mon, price, n(40_000_000_000)).is_ok());
    }

    #[test]
    fn notional_floors_twice_like_the_venue() {
        let w = m("WETH-USDC");
        // 0.0000000001 WETH: the inner floor wipes the fraction the float would keep
        assert_eq!(quote_notional(&w, n(275_001), n(1)), U256::ZERO);
        assert!(check_order(&w, n(275_001), n(1)).unwrap_err().contains("minimum"));
    }

    #[test]
    fn bounds_and_ticks_are_enforced() {
        let w = m("WETH-USDC");
        assert!(check_order(&w, U256::ZERO, n(5_000_000_000)).is_err());
        assert!(check_order(&w, n(275_000), U256::ZERO).is_err());
        assert!(check_order(&w, n(u32::MAX as u128 + 1), n(5_000_000_000)).unwrap_err().contains("uint32"));
        let huge = (U256::from(1u64) << 96) + U256::from(1u64);
        assert!(check_order(&w, n(275_000), huge).unwrap_err().contains("uint96"));
        // 5e12 max notional: ~1.8M WETH at 2750
        assert!(check_order(&w, n(275_000), n(100_000_000_000_000_000)).unwrap_err().contains("maximum"));
    }

    #[test]
    fn post_only_crossing_is_caught_before_submission() {
        let (bid, ask) = (275_000u32, 275_300u32);
        assert!(crosses(Side::Buy, n(275_300), bid, ask), "buy at the ask crosses");
        assert!(!crosses(Side::Buy, n(275_299), bid, ask));
        assert!(crosses(Side::Sell, n(275_000), bid, ask), "sell at the bid crosses");
        assert!(!crosses(Side::Sell, n(275_001), bid, ask));
        // sentinels: an empty side can never be crossed
        assert!(!crosses(Side::Buy, n(999_999), bid, NO_ASK));
        assert!(!crosses(Side::Sell, n(1), NO_BID, ask));
    }

    #[test]
    fn limit_calldata_targets_the_market_and_round_trips() {
        let w = m("WETH-USDC");
        let coid = [7u8; 32];
        let c = limit_call(&w, 42, Side::Buy, n(275_000), n(5_000_000_000), TIF_GTC, true, coid).unwrap();
        assert_eq!(c.to, w.market_address, "to is the market, never a request-supplied address");
        assert_eq!(c.value, U256::ZERO, "orders never carry value: AccountCore holds the funds");
        assert_eq!(c.data[..4], batchCall::SELECTOR);
        assert_eq!(hex_sel(&c.data), "6947f147");

        let d = batchCall::abi_decode(&c.data).unwrap();
        assert_eq!(d.userId, U40::from(42u64));
        assert_eq!(d.orders.len(), 1);
        assert_eq!(d.orders[0].side, SIDE_BUY);
        assert_eq!(d.orders[0].price, 275_000);
        assert_eq!(d.orders[0].quantity, U96::from(5_000_000_000u64));
        assert_eq!(d.orders[0].tif, TIF_GTC);
        assert_eq!(d.orders[0].executionInstruction, EXEC_POST_ONLY);
        assert!(d.cancelSlotIdxs.is_empty());
        assert_eq!(d.clientOrderId.0, coid);

        let sell = limit_call(&w, 42, Side::Sell, n(275_000), n(5_000_000_000), TIF_GTC, false, coid).unwrap();
        let d = batchCall::abi_decode(&sell.data).unwrap();
        assert_eq!(d.orders[0].side, SIDE_SELL);
        assert_eq!(d.orders[0].executionInstruction, EXEC_NONE);
    }

    #[test]
    fn post_only_requires_gtc() {
        let w = m("WETH-USDC");
        let err = limit_call(&w, 1, Side::Buy, n(275_000), n(5_000_000_000), 1, true, [0; 32]).unwrap_err();
        assert!(err.contains("gtc"), "{err}");
    }

    #[test]
    fn cancel_is_slot_indexed_and_bounded() {
        let w = m("WETH-USDC");
        let c = cancel_call(&w, 9, 61, [0; 32]).unwrap();
        let d = batchCall::abi_decode(&c.data).unwrap();
        assert!(d.orders.is_empty());
        assert_eq!(d.cancelSlotIdxs, vec![61]);
        assert!(cancel_call(&w, 9, 62, [0; 32]).is_err(), "there are 62 maker slots, 0..61");
    }

    #[test]
    fn swap_calldata_is_raw_atoms_on_the_market() {
        let mon = m("MON-USDC");
        let c = swap_call(&mon, 3, Side::Buy, n(10_000_000), n(9_000_000), 1_790_000_000).unwrap();
        assert_eq!(c.to, mon.market_address);
        assert_eq!(c.value, U256::ZERO, "the v2 swap never carries msg.value");
        assert_eq!(hex_sel(&c.data), "9d49a2c5");
        let d = swapCall::abi_decode(&c.data).unwrap();
        assert_eq!((d.userId, d.isBuy, d.amountIn, d.minAmountOut), (U40::from(3u64), true, 10_000_000, 9_000_000));
        assert!(!swapCall::abi_decode(&swap_call(&mon, 3, Side::Sell, n(1), n(0), 0).unwrap().data).unwrap().isBuy);
    }

    #[test]
    fn min_out_applies_slippage_downward() {
        let q = n(1_000_000);
        assert_eq!(min_out(q, 50), n(995_000));
        assert_eq!(min_out(q, 300), n(970_000));
        assert!(min_out(q, 10) < q, "protection must always bite");
        assert_eq!(min_out(U256::ZERO, 50), U256::ZERO);
    }

    #[test]
    fn tif_parses_strictly() {
        assert_eq!(tif("GTC"), Some(0));
        assert_eq!(tif(""), Some(0));
        assert_eq!(tif("ioc"), Some(1));
        assert_eq!(tif(" fok "), Some(2));
        assert_eq!(tif("market"), None, "there is no market-order enum");
    }

    /// selectors pinned against the deployed OrderBook implementation
    /// 0xE20f57e673d7F254279c19270862A3d1E6F5B0d4.
    #[test]
    fn selectors_match_the_deployed_implementation() {
        for (got, want) in [
            (batchCall::SELECTOR, "6947f147"),
            (swapCall::SELECTOR, "9d49a2c5"),
            (estimateSwapCall::SELECTOR, "94eac407"),
            (getOrderIdCall::SELECTOR, "471bccbb"),
            (bestBidAskCall::SELECTOR, "b4de8b70"),
        ] {
            assert_eq!(alloy_primitives::hex::encode(got), want);
        }
    }

    /// the exact bytes MON/USDC accepted in an eth_call simulation (400 MON @
    /// 0.025 for account 2, post-only GTC, coid 0x..01). 100 MON at the same
    /// price reverts 0x0a5c4f1f: 2.50 USDC is under the 10 USDC floor.
    #[test]
    fn limit_calldata_matches_what_the_market_accepted() {
        let mut coid = [0u8; 32];
        coid[31] = 1;
        let c = limit_call(&m("MON-USDC"), 2, Side::Buy, n(25_000), n(40_000_000_000), TIF_GTC, true, coid).unwrap();
        assert_eq!(format!("0x{}", alloy_primitives::hex::encode(&c.data)), GOLDEN_BATCH);
    }

    const GOLDEN_BATCH: &str = "0x6947f14700000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000080000000000000000000000000000000000000000000000000000000000000016000000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000001000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000009502f900000000000000000000000000000000000000000000000000000000000000061a80000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000";

    fn hex_sel(data: &[u8]) -> String {
        alloy_primitives::hex::encode(&data[..4])
    }
}
