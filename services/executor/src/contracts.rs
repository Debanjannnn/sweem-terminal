use alloy::{
    primitives::{aliases::{U40, U96}, Address, B256, U256},
    sol,
    sol_types::{SolCall, SolEvent},
};
use serde::Deserialize;

use crate::rpc::Log;

sol! {
    struct NativeOrder { uint8 side; uint96 quantity; uint32 price; uint8 tif; uint8 executionInstruction; uint32 minSizeAfterBlock; }
    // 0x6947f147: cancels run before orders, one tx
    function batch(uint40 userId, NativeOrder[] orders, uint8[] cancelSlotIdxs, bytes32 clientOrderId) external;
    function getOrderId(uint40 userId, uint8 slotIdx) external view returns (uint64);
    function baseToken() external view returns (address);
    event BookUpdatesPacked(uint40 indexed accountId, address indexed executor, bytes32 clientOrderId, bytes packedUpdates);

    function deposit(address token, uint256 amount) external payable;
    function userRegistry(address user) external view returns (uint40);
    function getBalance(address user, address token) external view returns (uint256);
    function getSpotReservedBalance(address user, address token) external view returns (uint256);

    function approve(address spender, uint256 amount) external returns (bool);
    function allowance(address owner, address spender) external view returns (uint256);

    function mint(uint256 usdcIn, uint256 minShares, uint256 deadline) external returns (uint256);
    function redeem(uint256 shares, uint256 minUsdcOut, uint256 deadline) external returns (uint256);
    function baskets() external view returns (address[]);

    error Slippage();
}

#[derive(Debug, Clone, Deserialize)]
pub struct OrderReq {
    pub side: String,
    pub price: String,
    pub size: String,
    pub tif: String,
}

pub fn native_order(o: &OrderReq) -> Result<NativeOrder, String> {
    let side = match o.side.as_str() { "buy" => 0, "sell" => 1, s => return Err(format!("side {s}")) };
    let tif = match o.tif.as_str() { "gtc" => 0, "ioc" => 1, t => return Err(format!("tif {t}")) };
    let price: u32 = o.price.parse().map_err(|_| format!("price {}", o.price))?;
    let quantity: U96 = o.size.parse().map_err(|_| format!("size {}", o.size))?;
    if price == 0 || quantity.is_zero() {
        return Err("zero price or size".into());
    }
    Ok(NativeOrder { side, quantity, price, tif, executionInstruction: 0, minSizeAfterBlock: 0 })
}

pub fn place_orders(account_id: u64, orders: Vec<NativeOrder>, cancel_slots: Vec<u8>) -> Vec<u8> {
    batchCall { userId: U40::from(account_id), orders, cancelSlotIdxs: cancel_slots, clientOrderId: B256::ZERO }.abi_encode()
}

/// one 39-byte BookUpdatesPacked record.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct BookUpdate {
    pub user_id: u64,
    pub slot: u8,
    pub live: bool,
    pub order_id: u64,
}

pub fn parse_updates(packed: &[u8]) -> Result<Vec<BookUpdate>, String> {
    if packed.len() % 39 != 0 {
        return Err(format!("BookUpdatesPacked length {} not a multiple of 39", packed.len()));
    }
    let be = |b: &[u8]| b.iter().fold(0u64, |a, &x| (a << 8) | x as u64);
    Ok(packed
        .chunks(39)
        .map(|r| BookUpdate { user_id: be(&r[0..5]), slot: r[5], live: r[6] & 0x80 != 0, order_id: be(&r[23..31]) })
        .collect())
}

/// this account's slot changes in a receipt, in log order.
pub fn book_updates(logs: &[Log], market: Address, account_id: u64) -> Vec<BookUpdate> {
    logs.iter()
        .filter(|l| l.address == market && l.topics.first() == Some(&BookUpdatesPacked::SIGNATURE_HASH))
        .filter_map(|l| BookUpdatesPacked::decode_raw_log(l.topics.iter().copied(), &l.data).ok())
        .flat_map(|e| parse_updates(&e.packedUpdates).unwrap_or_default())
        .filter(|u| u.user_id == account_id)
        .collect()
}

pub fn u256_str(s: &str) -> Result<U256, String> {
    U256::from_str_radix(s, 10).map_err(|_| format!("not an unsigned integer: {s}"))
}

#[cfg(test)]
mod tests {
    use super::*;
    use alloy::primitives::hex;

    #[test]
    fn selectors() {
        assert_eq!(hex::encode(batchCall::SELECTOR), "6947f147");
        assert_eq!(hex::encode(depositCall::SELECTOR), "47e7ef24");
        assert_eq!(hex::encode(getOrderIdCall::SELECTOR), "471bccbb");
        assert_eq!(hex::encode(getSpotReservedBalanceCall::SELECTOR), "da19598d");
        assert_eq!(
            BookUpdatesPacked::SIGNATURE_HASH.to_string(),
            "0x6e2244ce87961f72e8c2ee9b591b21e76de1f24c3bea5f2a46f4220b0b33b696"
        );
    }

    // golden: `cast calldata 'batch(uint40,(uint8,uint96,uint32,uint8,uint8,uint32)[],uint8[],bytes32)' 42 '[(0,5000000000,299000,0,0,0),(1,5000000000,301000,0,0,0)]' '[3,7]' 0x00..00`
    const GOLDEN: &str = "6947f147000000000000000000000000000000000000000000000000000000000000002a00000000000000000000000000000000000000000000000000000000000000800000000000000000000000000000000000000000000000000000000000000220000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000012a05f2000000000000000000000000000000000000000000000000000000000000048ff80000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001000000000000000000000000000000000000000000000000000000012a05f20000000000000000000000000000000000000000000000000000000000000497c8000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000030000000000000000000000000000000000000000000000000000000000000007";

    #[test]
    fn place_orders_matches_cast_golden() {
        let req: serde_json::Value = serde_json::from_str(include_str!("../../../testdata/contracts/executor/bot_orders.request.json")).unwrap();
        let orders: Vec<OrderReq> = serde_json::from_value(req["orders"].clone()).unwrap();
        let native = orders.iter().map(native_order).collect::<Result<Vec<_>, _>>().unwrap();
        assert_eq!(hex::encode(place_orders(42, native, vec![3, 7])), GOLDEN);
    }

    #[test]
    fn order_validation() {
        let o = |side: &str, tif: &str, price: &str| OrderReq { side: side.into(), price: price.into(), size: "1".into(), tif: tif.into() };
        assert!(native_order(&o("buy", "gtc", "1")).is_ok());
        assert_eq!(native_order(&o("sell", "ioc", "1")).unwrap().tif, 1);
        assert!(native_order(&o("long", "gtc", "1")).is_err());
        assert!(native_order(&o("buy", "fok", "1")).is_err());
        assert!(native_order(&o("buy", "gtc", "4294967296")).is_err(), "price is uint32");
        assert!(native_order(&o("buy", "gtc", "0")).is_err());
    }

    // real log from kuru-spot-v2.md §10.1: WETH/USDC block 64699421 log 10
    #[test]
    fn parses_real_book_update() {
        let rec = hex::decode("00000000480080000433c800000000000000003178cb80000000000002e8280000000000000fa0").unwrap();
        assert_eq!(parse_updates(&rec).unwrap(), vec![BookUpdate { user_id: 72, slot: 0, live: true, order_id: 190504 }]);
        assert!(parse_updates(&rec[..38]).is_err());
    }
}
