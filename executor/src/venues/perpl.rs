//! Perpl perps, traded directly on the Exchange contract from the account's own
//! wallet. No API key: direct calls are not gated by allowOrderForwarding, and
//! the API-key path needs Perpl to whitelist our origin. Every id, decimal and
//! address comes from /api/v1/pub/context at runtime; only the Exchange address
//! is pinned, and the context must agree with it.
use alloy_primitives::{Address, B256, U256};
use alloy_sol_types::{sol, SolCall, SolEvent};
use serde::Deserialize;
use std::{
    sync::Mutex,
    time::{Duration, Instant},
};

use crate::{rpc::Rpc, venues::Call};

#[path = "perpl_errors.rs"]
mod errors;

sol! {
    struct OrderDesc {
        uint256 orderDescId; uint256 perpId; uint8 orderType; uint256 orderId;
        uint256 pricePNS; uint256 lotLNS; uint256 expiryBlock; bool postOnly; bool fillOrKill;
        bool immediateOrCancel; uint256 maxMatches; uint256 leverageHdths; uint256 lastExecutionBlock;
        uint256 amountCNS; uint256 maxNegPnlCollatBPS;
    }
    struct OrderSignature { uint256 perpId; uint256 orderId; }
    struct PositionBitMap { uint256 bank1; uint256 bank2; uint256 bank3; uint256 bank4; }
    struct AccountInfo {
        uint256 accountId; uint256 balanceCNS; uint256 lockedBalanceCNS; uint8 frozen;
        address accountAddr; PositionBitMap positions;
    }
    struct PositionInfoV2 {
        uint256 accountId; uint256 nextNodeId; uint256 prevNodeId; uint8 positionType;
        uint256 depositCNS; uint256 pricePNS; uint256 lotLNS; uint256 entryBlock; int256 pnlCNS;
        int256 deltaPnlCNS; int256 premiumPnlCNS; uint256 priceResiduePNSQ16;
    }
    struct OrderV2 {
        uint32 accountId; uint8 orderType; uint24 priceONS; uint40 lotLNS; uint16 recycleFeeRaw;
        uint32 expiryBlock; uint16 leverageHdths; uint16 orderId; uint16 prevOrderId; uint16 nextOrderId;
        uint16 maxNegPnlCollatBPS; uint8 builderId; uint16 builderFeePer100K;
    }

    function execOrder(OrderDesc orderDesc) external returns (OrderSignature);
    function createAccount(uint256 amountCNS) external returns (uint256 accountId);
    function depositCollateral(uint256 amountCNS) external;
    function withdrawCollateral(uint256 amountCNS) external;
    function getAccountByAddr(address accountAddress) external view returns (AccountInfo);
    function getPositionV2(uint256 perpId, uint256 accountId) external view
        returns (PositionInfoV2 positionInfo, uint256 markPricePNS, bool markPriceValid);
    function getOrderV2(uint256 perpId, uint256 orderId) external view returns (OrderV2);

    event OrderPlaced(uint256 orderId, uint256 lotLNS, uint256 lockedBalanceCNS, int256 amountCNS, uint256 balanceCNS);
    event OrderCancelled(uint256 lockedBalanceCNS, int256 amountCNS, uint256 balanceCNS);
    event ImmediateOrCancelExecuted(uint256 unmatchedLotLNS, uint256 totalLotLNS);
    event TakerOrderFilledV2(uint256 entryPricePNS, uint256 collatPricePNS, uint256 pnlPricePNS, uint256 lotLNS,
        uint256 feeCNS, int256 amountCNS, uint256 balanceCNS, uint256 builderId, uint256 builderFeeCNS);
}

// contract order types, 0-based (the REST/WS API is 1-based)
pub const OPEN_LONG: u8 = 0;
pub const OPEN_SHORT: u8 = 1;
pub const CLOSE_LONG: u8 = 2;
pub const CLOSE_SHORT: u8 = 3;
pub const CANCEL: u8 = 4;
// contract position types
pub const LONG: u8 = 0;

/// 4-byte selector of AccountDoesNotExist(address): getAccountByAddr's answer
/// for a wallet that never opened an account.
const NO_ACCOUNT: [u8; 4] = [0x03, 0xa0, 0xe2, 0x77];

// ---------------------------------------------------------------- context

#[derive(Debug, Clone, Deserialize)]
pub struct Context {
    pub chain: ChainInfo,
    pub instances: Vec<Instance>,
    pub tokens: Vec<Token>,
    pub markets: Vec<Perp>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct ChainInfo {
    pub chain_id: u64,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Instance {
    pub id: u64,
    pub address: Address,
    pub collateral_token_id: u64,
    #[serde(with = "dec")]
    pub min_account_open_amount: U256,
    #[serde(with = "dec")]
    pub min_deposit_amount: U256,
    #[serde(with = "dec")]
    pub min_withdraw_amount: U256,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Token {
    pub id: u64,
    pub address: Address,
    pub symbol: String,
    pub decimals: u32,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Perp {
    pub id: u64,
    pub instance_id: u64,
    pub symbol: String,
    pub order_ttl_blocks: u64,
    pub order_max_market_slippage_bps: u32,
    pub order_max_neg_pnl_collat_bps: u32,
    pub config: PerpConfig,
    pub state: PerpState,
}

#[derive(Debug, Clone, Deserialize)]
pub struct PerpConfig {
    pub is_open: bool,
    pub price_decimals: u32,
    pub size_decimals: u32,
    /// max leverage in hundredths: 1500 = 15x (sdk rejects leverage > this)
    pub initial_margin: u64,
    /// leverage in hundredths at which a position is liquidated: 2500 = 4%
    pub maintenance_margin: u64,
}

/// live prices in PNS, a few seconds old
#[derive(Debug, Clone, Default, Deserialize)]
pub struct PerpState {
    #[serde(default)]
    pub mrk: u64,
    #[serde(default)]
    pub bid: u64,
    #[serde(default)]
    pub ask: u64,
}

mod dec {
    use super::*;
    pub fn deserialize<'de, D: serde::Deserializer<'de>>(d: D) -> Result<U256, D::Error> {
        let s = String::deserialize(d)?;
        U256::from_str_radix(s.trim(), 10).map_err(serde::de::Error::custom)
    }
}

/// what one Exchange looks like once the context has been checked against the pin
#[derive(Debug, Clone)]
pub struct Venue {
    pub chain_id: u64,
    pub exchange: Address,
    pub collateral: Token,
    pub instance: Instance,
    pub perps: Vec<Perp>,
}

impl Context {
    /// refuses a context that names another exchange than the pinned one: the
    /// context is fetched over http, the pin is the only address we trust.
    pub fn venue(&self, pinned: Address) -> Result<Venue, String> {
        let instance = self
            .instances
            .iter()
            .find(|i| i.address == pinned)
            .ok_or_else(|| format!("perpl context names no instance at the pinned exchange {pinned}"))?
            .clone();
        let collateral = self
            .tokens
            .iter()
            .find(|t| t.id == instance.collateral_token_id)
            .ok_or("perpl context names no collateral token for the pinned exchange")?
            .clone();
        let perps = self.markets.iter().filter(|m| m.instance_id == instance.id).cloned().collect();
        Ok(Venue { chain_id: self.chain.chain_id, exchange: pinned, collateral, instance, perps })
    }
}

impl Venue {
    pub fn perp(&self, id: &str) -> Result<&Perp, String> {
        let want: u64 = id.trim().parse().map_err(|_| format!("perpl market id {id:?} is not a number"))?;
        let p = self.perps.iter().find(|p| p.id == want).ok_or_else(|| format!("perpl has no market {want}"))?;
        if !p.config.is_open {
            return Err(format!("perpl market {} ({}) is closed", p.id, p.symbol));
        }
        Ok(p)
    }
}

/// fetches /api/v1/pub/context, cached briefly: the book prices in it are what a
/// market order is bounded by, so it is never older than a few seconds.
pub struct Client {
    http: reqwest::Client,
    url: String,
    pub pinned: Address,
    cache: Mutex<Option<(Instant, Venue)>>,
}

const CONTEXT_TTL: Duration = Duration::from_secs(3);

impl Client {
    pub fn new(api_url: &str, pinned: Address) -> Self {
        Self {
            http: reqwest::Client::builder().timeout(Duration::from_secs(10)).build().unwrap_or_default(),
            url: format!("{}/v1/pub/context", api_url.trim_end_matches('/')),
            pinned,
            cache: Mutex::new(None),
        }
    }

    pub async fn venue(&self) -> Result<Venue, String> {
        if let Some((at, v)) = self.cache.lock().unwrap().as_ref() {
            if at.elapsed() < CONTEXT_TTL {
                return Ok(v.clone());
            }
        }
        let resp = self.http.get(&self.url).send().await.map_err(|e| format!("perpl context unreachable: {e}"))?;
        if !resp.status().is_success() {
            return Err(format!("perpl context answered HTTP {}", resp.status()));
        }
        let ctx: Context = resp.json().await.map_err(|e| format!("perpl context did not parse: {e}"))?;
        let v = ctx.venue(self.pinned)?;
        *self.cache.lock().unwrap() = Some((Instant::now(), v.clone()));
        Ok(v)
    }
}

// ---------------------------------------------------------------- pure math

/// leverage in hundredths, bounded by the market. 0 means "the market's max,
/// capped at 10x": a default nobody picked should not be the riskiest one.
pub fn leverage(requested: u64, p: &Perp) -> Result<u64, String> {
    let max = p.config.initial_margin;
    let lev = if requested == 0 { max.min(1000) } else { requested };
    if lev < 100 || lev > max {
        return Err(format!("leverage {}x is outside 1x..{}x for {}", lev as f64 / 100.0, max as f64 / 100.0, p.symbol));
    }
    Ok(lev)
}

/// the worst price a taker order may fill at. perpl has no price-0 market order:
/// a market order is an IOC with this bound, so slippage is enforced on chain.
pub fn worst_price(long: bool, p: &Perp, slippage_bps: u32) -> Result<U256, String> {
    let bps = slippage_bps.clamp(1, p.order_max_market_slippage_bps.max(1)) as u128;
    let s = &p.state;
    let reference = if long { if s.ask > 0 { s.ask } else { s.mrk } } else if s.bid > 0 { s.bid } else { s.mrk };
    if reference == 0 {
        return Err(format!("perpl market {} has no live price to bound a market order", p.symbol));
    }
    let r = reference as u128;
    Ok(U256::from(if long { (r * (10_000 + bps)).div_ceil(10_000) } else { r * (10_000 - bps) / 10_000 }))
}

/// isolated-margin liquidation estimate in PNS, ignoring funding and fees: the
/// price at which deposit + pnl falls to notional / maintenance leverage.
/// perpl publishes no formula, so this is labelled an estimate wherever shown.
pub fn liquidation_price(long: bool, entry_pns: U256, lot_lns: U256, deposit_cns: U256, p: &Perp, collateral_decimals: u32) -> Option<U256> {
    if lot_lns.is_zero() || p.config.maintenance_margin == 0 {
        return None;
    }
    let f = |v: U256, d: u32| v.to::<u128>() as f64 / 10f64.powi(d as i32);
    let entry = f(entry_pns, p.config.price_decimals);
    let lot = f(lot_lns, p.config.size_decimals);
    let deposit = f(deposit_cns, collateral_decimals);
    let mm = 100.0 / p.config.maintenance_margin as f64;
    let price = if long { (entry * lot - deposit) / (lot * (1.0 - mm)) } else { (entry * lot + deposit) / (lot * (1.0 + mm)) };
    (price.is_finite() && price > 0.0).then(|| U256::from((price * 10f64.powi(p.config.price_decimals as i32)).round() as u128))
}

/// perp ids held by an account: bank1 bits 0..=252, then 253 per bank.
pub fn position_perps(b: &PositionBitMap) -> Vec<u64> {
    let mut out = Vec::new();
    for (bank, word) in [b.bank1, b.bank2, b.bank3, b.bank4].into_iter().enumerate() {
        for bit in 0..256usize {
            if word.bit(bit) {
                out.push((bank * 253 + bit) as u64);
            }
        }
    }
    out
}

/// names a revert: the node's error string carries the 4-byte selector.
pub fn revert_reason(err: &str) -> String {
    let lower = err.to_ascii_lowercase();
    for (sel, name) in errors::ERRORS {
        let hex = format!("0x{}", alloy_primitives::hex::encode(sel));
        if lower.contains(&hex) {
            return format!("{name} ({hex})");
        }
    }
    err.to_string()
}

// ---------------------------------------------------------------- calls

pub struct OrderSpec {
    pub client_id: u64,
    pub perp_id: u64,
    pub order_type: u8,
    pub order_id: u64,
    pub price: U256,
    pub lot: U256,
    pub post_only: bool,
    pub ioc: bool,
    pub leverage: u64,
    pub last_block: u64,
    pub max_neg_pnl_bps: u32,
}

pub fn order_call(exchange: Address, o: &OrderSpec) -> Call {
    let desc = OrderDesc {
        orderDescId: U256::from(o.client_id),
        perpId: U256::from(o.perp_id),
        orderType: o.order_type,
        orderId: U256::from(o.order_id),
        pricePNS: o.price,
        lotLNS: o.lot,
        expiryBlock: U256::ZERO,
        postOnly: o.post_only,
        fillOrKill: false,
        immediateOrCancel: o.ioc,
        maxMatches: U256::ZERO,
        leverageHdths: U256::from(o.leverage),
        lastExecutionBlock: U256::from(o.last_block),
        amountCNS: U256::ZERO,
        maxNegPnlCollatBPS: U256::from(o.max_neg_pnl_bps),
    };
    Call { to: exchange, value: U256::ZERO, data: execOrderCall { orderDesc: desc }.abi_encode().into() }
}

pub fn create_account_call(exchange: Address, amount: U256) -> Call {
    Call { to: exchange, value: U256::ZERO, data: createAccountCall { amountCNS: amount }.abi_encode().into() }
}

pub fn deposit_call(exchange: Address, amount: U256) -> Call {
    Call { to: exchange, value: U256::ZERO, data: depositCollateralCall { amountCNS: amount }.abi_encode().into() }
}

pub fn withdraw_call(exchange: Address, amount: U256) -> Call {
    Call { to: exchange, value: U256::ZERO, data: withdrawCollateralCall { amountCNS: amount }.abi_encode().into() }
}

// ---------------------------------------------------------------- reads

/// None when the wallet has never opened an account.
pub async fn account(rpc: &Rpc, exchange: Address, who: Address) -> Result<Option<AccountInfo>, String> {
    let data = getAccountByAddrCall { accountAddress: who }.abi_encode();
    match rpc.eth_call(who, exchange, U256::ZERO, &data).await {
        Ok(out) => getAccountByAddrCall::abi_decode_returns(&out).map(Some).map_err(|e| e.to_string()),
        Err(e) if e.to_ascii_lowercase().contains(&format!("0x{}", alloy_primitives::hex::encode(NO_ACCOUNT))) => Ok(None),
        Err(e) => Err(format!("perpl account read failed: {}", revert_reason(&e))),
    }
}

pub async fn position(rpc: &Rpc, exchange: Address, perp: u64, account_id: U256) -> Result<getPositionV2Return, String> {
    let data = getPositionV2Call { perpId: U256::from(perp), accountId: account_id }.abi_encode();
    let out = rpc.eth_call(exchange, exchange, U256::ZERO, &data).await.map_err(|e| revert_reason(&e))?;
    getPositionV2Call::abi_decode_returns(&out).map_err(|e| e.to_string())
}

pub async fn order(rpc: &Rpc, exchange: Address, perp: u64, order_id: u64) -> Result<OrderV2, String> {
    let data = getOrderV2Call { perpId: U256::from(perp), orderId: U256::from(order_id) }.abi_encode();
    let out = rpc.eth_call(exchange, exchange, U256::ZERO, &data).await.map_err(|e| revert_reason(&e))?;
    getOrderV2Call::abi_decode_returns(&out).map_err(|e| e.to_string())
}

// ---------------------------------------------------------------- outcome

#[derive(Debug, Clone)]
pub struct Log {
    pub address: Address,
    pub topics: Vec<B256>,
    pub data: Vec<u8>,
}

/// what one execOrder did, read from its own receipt. Taker, IOC and placement
/// events in a single execOrder are the sender's; maker fills belong to others.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct Outcome {
    pub filled_lot: U256,
    /// sum(price * lot), for the average fill price
    pub filled_value: U256,
    pub fee: U256,
    pub unmatched_lot: Option<U256>,
    pub rested_order_id: Option<u64>,
    pub rested_lot: U256,
    pub canceled: bool,
}

impl Outcome {
    pub fn from_logs(exchange: Address, logs: &[Log]) -> Self {
        let mut o = Outcome::default();
        for l in logs.iter().filter(|l| l.address == exchange) {
            let Some(t0) = l.topics.first() else { continue };
            if *t0 == TakerOrderFilledV2::SIGNATURE_HASH {
                if let Ok(e) = TakerOrderFilledV2::decode_raw_log(l.topics.iter().copied(), &l.data) {
                    o.filled_lot += e.lotLNS;
                    o.filled_value += e.entryPricePNS * e.lotLNS;
                    o.fee += e.feeCNS;
                }
            } else if *t0 == ImmediateOrCancelExecuted::SIGNATURE_HASH {
                if let Ok(e) = ImmediateOrCancelExecuted::decode_raw_log(l.topics.iter().copied(), &l.data) {
                    o.unmatched_lot = Some(e.unmatchedLotLNS);
                }
            } else if *t0 == OrderPlaced::SIGNATURE_HASH {
                if let Ok(e) = OrderPlaced::decode_raw_log(l.topics.iter().copied(), &l.data) {
                    o.rested_order_id = Some(e.orderId.to::<u64>());
                    o.rested_lot = e.lotLNS;
                }
            } else if *t0 == OrderCancelled::SIGNATURE_HASH {
                o.canceled = true;
            }
        }
        o
    }

    pub fn avg_price(&self) -> Option<U256> {
        (!self.filled_lot.is_zero()).then(|| self.filled_value / self.filled_lot)
    }

    /// the truthful word for what happened. a landed IOC that matched nothing is
    /// not an error, but it is not a fill either.
    pub fn status(&self, requested_lot: U256) -> &'static str {
        if self.canceled {
            "canceled"
        } else if self.rested_order_id.is_some() {
            "resting"
        } else if self.filled_lot.is_zero() {
            "not_filled"
        } else if self.filled_lot < requested_lot || self.unmatched_lot.is_some_and(|u| !u.is_zero()) {
            "partial"
        } else {
            "filled"
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use alloy_primitives::{hex, keccak256};

    fn ctx() -> Context {
        serde_json::from_str(include_str!("testdata/perpl_context.json")).expect("live context parses")
    }

    const EXCHANGE: Address = alloy_primitives::address!("1964c32f0be608e7d29302aff5e61268e72080cc");

    #[test]
    fn selectors_match_the_deployed_exchange() {
        // from perpl-sdk abi methodIdentifiers, and seen live in eth_call sims
        assert_eq!(execOrderCall::SELECTOR, hex!("4d8dc985"));
        assert_eq!(createAccountCall::SELECTOR, hex!("cab13915"));
        assert_eq!(depositCollateralCall::SELECTOR, hex!("bad4a01f"));
        assert_eq!(withdrawCollateralCall::SELECTOR, hex!("6112fe2e"));
        assert_eq!(getAccountByAddrCall::SELECTOR, hex!("12e8eb2c"));
        assert_eq!(getPositionV2Call::SELECTOR, hex!("ea3196ec"));
        assert_eq!(&NO_ACCOUNT, &keccak256("AccountDoesNotExist(address)")[..4]);
    }

    #[test]
    fn live_context_resolves_the_pinned_exchange_only() {
        let v = ctx().venue(EXCHANGE).unwrap();
        assert_eq!(v.chain_id, 10143);
        assert_eq!(v.collateral.symbol, "AUSD");
        assert_eq!(v.collateral.decimals, 6);
        assert_eq!(v.instance.min_account_open_amount, U256::from(100_000_000u64));
        let btc = v.perp("16").unwrap();
        assert_eq!((btc.config.price_decimals, btc.config.size_decimals), (1, 5));
        assert!(v.perp("999").is_err());
        assert!(ctx().venue(Address::repeat_byte(0xee)).is_err(), "a context for another exchange is refused");
    }

    #[test]
    fn leverage_is_bounded_by_the_market() {
        let v = ctx().venue(EXCHANGE).unwrap();
        let btc = v.perp("16").unwrap(); // initial_margin 1500 = 15x
        assert_eq!(leverage(0, btc).unwrap(), 1000, "default is 10x, not the max");
        assert_eq!(leverage(1500, btc).unwrap(), 1500);
        assert!(leverage(1501, btc).is_err());
        assert!(leverage(99, btc).is_err());
        let mon = v.perp("64").unwrap(); // 300 = 3x
        assert_eq!(leverage(0, mon).unwrap(), 300, "default never exceeds the market max");
    }

    #[test]
    fn worst_price_bounds_a_market_order_on_the_right_side() {
        let v = ctx().venue(EXCHANGE).unwrap();
        let btc = v.perp("16").unwrap();
        let (bid, ask) = (U256::from(btc.state.bid), U256::from(btc.state.ask));
        let long = worst_price(true, btc, 50).unwrap();
        let short = worst_price(false, btc, 50).unwrap();
        assert!(long > ask && short < bid);
        // a caller cannot widen past the market's cap (1000 bps on BTC)
        let wide = worst_price(true, btc, 9_000).unwrap();
        assert_eq!(wide, U256::from((btc.state.ask as u128 * 11_000).div_ceil(10_000)));
    }

    #[test]
    fn liquidation_estimate_sits_on_the_losing_side() {
        let v = ctx().venue(EXCHANGE).unwrap();
        let btc = v.perp("16").unwrap();
        // 0.1 BTC at 80000.0, 800 AUSD deposit = 10x
        let (entry, lot, dep) = (U256::from(800_000u64), U256::from(10_000u64), U256::from(800_000_000u64));
        let long = liquidation_price(true, entry, lot, dep, btc, 6).unwrap();
        let short = liquidation_price(false, entry, lot, dep, btc, 6).unwrap();
        assert!(long < entry && short > entry, "long {long} short {short}");
        // long: (8000 - 800) / (0.1 * 0.96) = 75000.0
        assert_eq!(long, U256::from(750_000u64));
    }

    #[test]
    fn bitmap_decodes_to_perp_ids() {
        let mut b = PositionBitMap { bank1: U256::ZERO, bank2: U256::ZERO, bank3: U256::ZERO, bank4: U256::ZERO };
        b.bank1.set_bit(16, true);
        b.bank1.set_bit(32, true);
        b.bank2.set_bit(3, true); // 253 + 3 = 256, ZEC
        assert_eq!(position_perps(&b), vec![16, 32, 256]);
    }

    #[test]
    fn reverts_are_named() {
        let e = r#"{"code":3,"message":"execution reverted","data":"0xbeb3942900000000"}"#;
        assert!(revert_reason(e).starts_with("CrossesBook"));
    }

    fn fixture() -> (Vec<Log>, String) {
        let v: serde_json::Value = serde_json::from_str(include_str!("testdata/perpl_ioc_long.json")).unwrap();
        let logs = v["logs"]
            .as_array()
            .unwrap()
            .iter()
            .map(|l| Log {
                address: l["address"].as_str().unwrap().parse().unwrap(),
                topics: l["topics"].as_array().unwrap().iter().map(|t| t.as_str().unwrap().parse().unwrap()).collect(),
                data: hex::decode(l["data"].as_str().unwrap()).unwrap(),
            })
            .collect();
        (logs, v["input"].as_str().unwrap().to_string())
    }

    // a live IOC long that crossed five makers, from a debug_traceCall sim
    #[test]
    fn a_live_ioc_fill_reads_as_filled() {
        let (logs, input) = fixture();
        let desc = execOrderCall::abi_decode(&hex::decode(input).unwrap()).unwrap().orderDesc;
        let o = Outcome::from_logs(EXCHANGE, &logs);
        assert!(!o.filled_lot.is_zero(), "taker fills were read");
        assert!(o.rested_order_id.is_none() && !o.canceled);
        assert!(o.filled_lot <= desc.lotLNS);
        let avg = o.avg_price().unwrap();
        assert!(avg <= desc.pricePNS, "a long never fills above its bound: avg {avg} bound {}", desc.pricePNS);
        assert!(matches!(o.status(desc.lotLNS), "filled" | "partial"));
        // logs from any other contract are ignored
        assert_eq!(Outcome::from_logs(Address::ZERO, &logs), Outcome::default());
    }

    /// the live Exchange accepts our encoding: a post-only bid below the book,
    /// simulated from the owner of the account in the IOC fixture. nothing is
    /// signed or sent. `cargo test -- --ignored live_`
    #[tokio::test]
    #[ignore]
    async fn live_exchange_accepts_our_order_encoding() {
        let rpc = Rpc::new("https://testnet-rpc.monad.xyz".into());
        let v = Client::new("https://testnet.perpl.xyz/api", EXCHANGE).venue().await.unwrap();
        let btc = v.perp("16").unwrap();
        let owner: Address = alloy_primitives::address!("a91f9339e65d6d0ded8861aa91de9e6ae9910cab");
        let acct = account(&rpc, EXCHANGE, owner).await.unwrap().expect("fixture owner has an account");
        let head = rpc.block_number().await.unwrap();
        let spec = OrderSpec {
            client_id: 1,
            perp_id: btc.id,
            order_type: OPEN_LONG,
            order_id: 0,
            price: U256::from(btc.state.bid * 9 / 10),
            lot: U256::from(10u64),
            post_only: true,
            ioc: false,
            leverage: leverage(0, btc).unwrap(),
            last_block: head + btc.order_ttl_blocks,
            max_neg_pnl_bps: btc.order_max_neg_pnl_collat_bps,
        };
        let call = order_call(EXCHANGE, &spec);
        let out = rpc.eth_call(owner, EXCHANGE, U256::ZERO, &call.data).await.map_err(|e| revert_reason(&e)).unwrap();
        let sig = execOrderCall::abi_decode_returns(&out).unwrap();
        assert_eq!(sig.perpId, U256::from(16u64));
        assert!(!sig.orderId.is_zero(), "a resting bid gets an order id");
        // and a wallet without an account reads as None, not an error
        assert!(account(&rpc, EXCHANGE, Address::repeat_byte(0x42)).await.unwrap().is_none());
        assert!(acct.balanceCNS > U256::ZERO);
    }

    #[test]
    fn status_words() {
        let lot = U256::from(10u64);
        assert_eq!(Outcome::default().status(lot), "not_filled");
        let partial = Outcome { filled_lot: U256::from(4u64), unmatched_lot: Some(U256::from(6u64)), ..Default::default() };
        assert_eq!(partial.status(lot), "partial");
        let full = Outcome { filled_lot: lot, unmatched_lot: Some(U256::ZERO), ..Default::default() };
        assert_eq!(full.status(lot), "filled");
        let rest = Outcome { rested_order_id: Some(7), ..Default::default() };
        assert_eq!(rest.status(lot), "resting");
    }
}
