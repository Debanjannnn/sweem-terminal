use crate::config::Config;
use alloy_primitives::{hex, Address, B256};
use alloy_sol_types::{sol, SolEvent};
use anyhow::Result;
use serde_json::{json, Value};

// Kuru V1 (docs are wrong: nothing indexed, price is uint256 1e18). Verified against mainnet logs.
sol! {
    event Trade(uint40 orderId, address makerAddress, bool isBuy, uint256 price, uint96 updatedSize, address takerAddress, address txOrigin, uint96 filledSize);
    event OrderCreated(uint40 orderId, address owner, uint96 size, uint32 price, bool isBuy);
    event OrdersCanceled(uint40[] orderId, address owner);
    event ActiveVaultDeposit(address user, uint256 baseAmount, uint256 quoteAmount, uint256 sharesMinted);
    event ActiveVaultWithdraw(address user, uint256 baseAmount, uint256 quoteAmount, uint256 sharesBurned);
    event Transfer(address indexed from, address indexed to, uint256 value);
    // Perpl: nothing indexed, positions keyed by accountId.
    event AccountCreated(address account, uint256 id);
    event CollateralDeposit(uint256 accountId, uint256 amountCNS, uint256 balanceCNS);
    event CollateralWithdrawal(uint256 accountId, uint256 amountCNS, uint256 balanceCNS);
    event PositionOpenedV2(uint256 perpId, uint256 accountId, uint8 positionType, uint256 leverageHdths, uint256 depositCNS, int256 pnlCollateralizedCNS, uint256 pricePNS, uint256 lotLNS, uint256 insFeeCNS, uint256 protFeeCNS, uint256 priceResiduePNSQ16);
    event PositionIncreasedV2(uint256 perpId, uint256 accountId, uint8 positionType, uint256 leverageHdths, uint256 startDepositCNS, uint256 endDepositCNS, int256 pnlCollateralizedCNS, int256 premiumPnlSettledCNS, uint256 maxNegPnlCollatBPS, uint256 pricePNS, uint256 startLotLNS, uint256 endLotLNS, uint256 insFeeCNS, uint256 protFeeCNS, uint256 priceResiduePNSQ16);
    event PositionDecreased(uint256 perpId, uint256 accountId, uint8 positionType, uint256 startDepositCNS, uint256 endDepositCNS, uint256 startLotLNS, uint256 endLotLNS, int256 deltaPnlCNS, int256 fundingCNS);
    event PositionClosed(uint256 perpId, uint256 accountId, uint8 positionType, uint256 pricePNS, int256 deltaPnlCNS, int256 fundingCNS);
}

mod aave {
    alloy_sol_types::sol! {
        event Supply(address indexed reserve, address user, address indexed onBehalfOf, uint256 amount, uint16 indexed referralCode);
        event Withdraw(address indexed reserve, address indexed user, address indexed to, uint256 amount);
    }
}

mod erc4626 {
    alloy_sol_types::sol! {
        event Deposit(address indexed sender, address indexed owner, uint256 assets, uint256 shares);
        event Withdraw(address indexed sender, address indexed receiver, address indexed owner, uint256 assets, uint256 shares);
    }
}

pub const KURU_TOPICS: [B256; 3] = [
    Trade::SIGNATURE_HASH,
    OrderCreated::SIGNATURE_HASH,
    OrdersCanceled::SIGNATURE_HASH,
];
pub const SUPPLY: B256 = aave::Supply::SIGNATURE_HASH;
pub const AAVE_WITHDRAW: B256 = aave::Withdraw::SIGNATURE_HASH;
pub const DEPOSIT_4626: B256 = erc4626::Deposit::SIGNATURE_HASH;
pub const WITHDRAW_4626: B256 = erc4626::Withdraw::SIGNATURE_HASH;
pub const ACTIVE_VAULT_TOPICS: [B256; 2] = [
    ActiveVaultDeposit::SIGNATURE_HASH,
    ActiveVaultWithdraw::SIGNATURE_HASH,
];
pub const TRANSFER: B256 = Transfer::SIGNATURE_HASH;
pub const PERPL_TOPICS: [B256; 7] = [
    AccountCreated::SIGNATURE_HASH,
    CollateralDeposit::SIGNATURE_HASH,
    CollateralWithdrawal::SIGNATURE_HASH,
    PositionOpenedV2::SIGNATURE_HASH,
    PositionIncreasedV2::SIGNATURE_HASH,
    PositionDecreased::SIGNATURE_HASH,
    PositionClosed::SIGNATURE_HASH,
];

#[derive(Debug, PartialEq)]
pub enum Event {
    Trade {
        order_id: i64,
        maker: String,
        taker: String,
        is_buy: bool,
        price: String,
        updated_size: String,
        filled_size: String,
    },
    OrderCreated {
        order_id: i64,
        owner: String,
        is_buy: bool,
        price: String,
        size: String,
    },
    OrdersCanceled {
        owner: String,
        ids: Vec<i64>,
    },
    PerplAccount {
        wallet: String,
        id: String,
    },
    /// Venue event whose wallet is known (`account` = wallet) or, for Perpl, an account id to resolve.
    Venue(Venue),
}

#[derive(Debug, PartialEq, Clone)]
pub struct Venue {
    pub venue: String,
    pub kind: &'static str,
    /// Wallet, or Perpl account id when `venue == "perpl"`.
    pub account: String,
    pub asset: Option<String>,
    pub amount: Option<String>,
    pub shares: Option<String>,
    pub extra: Option<Value>,
}

pub fn addr(a: impl AsRef<[u8]>) -> String {
    hex::encode_prefixed(a)
}

fn venue(
    venue: &str,
    kind: &'static str,
    account: String,
    asset: Option<String>,
    amount: String,
    shares: Option<String>,
) -> Event {
    Event::Venue(Venue {
        venue: venue.into(),
        kind,
        account,
        asset,
        amount: Some(amount),
        shares,
        extra: None,
    })
}

fn perp(kind: &'static str, account: String, perp_id: String, lot: String, extra: Value) -> Event {
    Event::Venue(Venue {
        venue: "perpl".into(),
        kind,
        account,
        asset: Some(format!("perp:{perp_id}")),
        amount: Some(lot),
        shares: None,
        extra: Some(extra),
    })
}

/// Decodes a log from one of the configured contracts. Unknown emitter/topic -> None.
pub fn decode(
    cfg: &Config,
    address: &Address,
    topics: &[B256],
    data: &[u8],
) -> Result<Option<Event>> {
    let Some(&t0) = topics.first() else {
        return Ok(None);
    };
    let at = addr(address);
    let t = || topics.iter().copied();
    if cfg.markets.contains(&at) {
        return Ok(Some(match t0 {
            Trade::SIGNATURE_HASH => {
                let e = Trade::decode_raw_log(t(), data)?;
                Event::Trade {
                    order_id: e.orderId.to::<u64>() as i64,
                    maker: addr(e.makerAddress),
                    taker: addr(e.takerAddress),
                    is_buy: e.isBuy,
                    price: e.price.to_string(),
                    updated_size: e.updatedSize.to_string(),
                    filled_size: e.filledSize.to_string(),
                }
            }
            OrderCreated::SIGNATURE_HASH => {
                let e = OrderCreated::decode_raw_log(t(), data)?;
                Event::OrderCreated {
                    order_id: e.orderId.to::<u64>() as i64,
                    owner: addr(e.owner),
                    is_buy: e.isBuy,
                    price: e.price.to_string(),
                    size: e.size.to_string(),
                }
            }
            OrdersCanceled::SIGNATURE_HASH => {
                let e = OrdersCanceled::decode_raw_log(t(), data)?;
                Event::OrdersCanceled {
                    owner: addr(e.owner),
                    ids: e.orderId.iter().map(|i| i.to::<u64>() as i64).collect(),
                }
            }
            _ => return Ok(None),
        }));
    }
    if let Some((name, _)) = cfg.pools.get(&at) {
        return Ok(Some(match t0 {
            SUPPLY => {
                let e = aave::Supply::decode_raw_log(t(), data)?;
                venue(
                    name,
                    "supply",
                    addr(e.onBehalfOf),
                    Some(addr(e.reserve)),
                    e.amount.to_string(),
                    None,
                )
            }
            // position owner is `user` (burns its aTokens); `to` only receives the underlying
            AAVE_WITHDRAW => {
                let e = aave::Withdraw::decode_raw_log(t(), data)?;
                venue(
                    name,
                    "withdraw",
                    addr(e.user),
                    Some(addr(e.reserve)),
                    e.amount.to_string(),
                    None,
                )
            }
            _ => return Ok(None),
        }));
    }
    if let Some((name, asset)) = cfg.vaults.get(&at) {
        return Ok(Some(match t0 {
            DEPOSIT_4626 => {
                let e = erc4626::Deposit::decode_raw_log(t(), data)?;
                venue(
                    name,
                    "deposit",
                    addr(e.owner),
                    Some(asset.clone()),
                    e.assets.to_string(),
                    Some(e.shares.to_string()),
                )
            }
            WITHDRAW_4626 => {
                let e = erc4626::Withdraw::decode_raw_log(t(), data)?;
                venue(
                    name,
                    "withdraw",
                    addr(e.owner),
                    Some(asset.clone()),
                    e.assets.to_string(),
                    Some(e.shares.to_string()),
                )
            }
            _ => return Ok(None),
        }));
    }
    if at == cfg.active_vault {
        let (kind, e) = match t0 {
            ActiveVaultDeposit::SIGNATURE_HASH => {
                let e = ActiveVaultDeposit::decode_raw_log(t(), data)?;
                (
                    "deposit",
                    (e.user, e.baseAmount, e.quoteAmount, e.sharesMinted),
                )
            }
            ActiveVaultWithdraw::SIGNATURE_HASH => {
                let e = ActiveVaultWithdraw::decode_raw_log(t(), data)?;
                (
                    "withdraw",
                    (e.user, e.baseAmount, e.quoteAmount, e.sharesBurned),
                )
            }
            _ => return Ok(None),
        };
        // amount = base (MON); quote (USDC) in extra
        return Ok(Some(Event::Venue(Venue {
            venue: "activevault".into(),
            kind,
            account: addr(e.0),
            asset: None,
            amount: Some(e.1.to_string()),
            shares: Some(e.3.to_string()),
            extra: Some(json!({"quote": e.2.to_string()})),
        })));
    }
    if at == cfg.perpl {
        return Ok(Some(match t0 {
            AccountCreated::SIGNATURE_HASH => {
                let e = AccountCreated::decode_raw_log(t(), data)?;
                Event::PerplAccount {
                    wallet: addr(e.account),
                    id: e.id.to_string(),
                }
            }
            CollateralDeposit::SIGNATURE_HASH => {
                let e = CollateralDeposit::decode_raw_log(t(), data)?;
                venue(
                    "perpl",
                    "collateral_deposit",
                    e.accountId.to_string(),
                    None,
                    e.amountCNS.to_string(),
                    None,
                )
            }
            CollateralWithdrawal::SIGNATURE_HASH => {
                let e = CollateralWithdrawal::decode_raw_log(t(), data)?;
                venue(
                    "perpl",
                    "collateral_withdraw",
                    e.accountId.to_string(),
                    None,
                    e.amountCNS.to_string(),
                    None,
                )
            }
            PositionOpenedV2::SIGNATURE_HASH => {
                let e = PositionOpenedV2::decode_raw_log(t(), data)?;
                perp(
                    "position_open",
                    e.accountId.to_string(),
                    e.perpId.to_string(),
                    e.lotLNS.to_string(),
                    json!({"positionType": e.positionType, "price": e.pricePNS.to_string(), "deposit": e.depositCNS.to_string(), "leverageHdths": e.leverageHdths.to_string()}),
                )
            }
            PositionIncreasedV2::SIGNATURE_HASH => {
                let e = PositionIncreasedV2::decode_raw_log(t(), data)?;
                perp(
                    "position_increase",
                    e.accountId.to_string(),
                    e.perpId.to_string(),
                    e.endLotLNS.to_string(),
                    json!({"positionType": e.positionType, "price": e.pricePNS.to_string(), "startLot": e.startLotLNS.to_string(), "deposit": e.endDepositCNS.to_string()}),
                )
            }
            PositionDecreased::SIGNATURE_HASH => {
                let e = PositionDecreased::decode_raw_log(t(), data)?;
                perp(
                    "position_decrease",
                    e.accountId.to_string(),
                    e.perpId.to_string(),
                    e.endLotLNS.to_string(),
                    json!({"positionType": e.positionType, "startLot": e.startLotLNS.to_string(), "deposit": e.endDepositCNS.to_string(), "pnl": e.deltaPnlCNS.to_string(), "funding": e.fundingCNS.to_string()}),
                )
            }
            PositionClosed::SIGNATURE_HASH => {
                let e = PositionClosed::decode_raw_log(t(), data)?;
                perp(
                    "position_close",
                    e.accountId.to_string(),
                    e.perpId.to_string(),
                    "0".into(),
                    json!({"positionType": e.positionType, "price": e.pricePNS.to_string(), "pnl": e.deltaPnlCNS.to_string(), "funding": e.fundingCNS.to_string()}),
                )
            }
            _ => return Ok(None),
        }));
    }
    if at == cfg.usdc && t0 == TRANSFER && !cfg.treasury.is_empty() {
        let e = Transfer::decode_raw_log(t(), data)?;
        if addr(e.to) == cfg.treasury {
            return Ok(Some(venue(
                "treasury",
                "fee",
                addr(e.from),
                Some(at),
                e.value.to_string(),
                None,
            )));
        }
    }
    Ok(None)
}

#[cfg(test)]
mod tests {
    use super::*;

    pub fn cfg() -> Config {
        std::env::set_var("DATABASE_URL", "unused");
        std::env::set_var("ENVIO_API_TOKEN", "unused");
        std::env::set_var(
            "DEPLOYMENTS_PATH",
            concat!(
                env!("CARGO_MANIFEST_DIR"),
                "/../../deployments/143.json"
            ),
        );
        let mut c = Config::from_env().unwrap();
        c.treasury = "0x9e2d2b510d0c0b68dd0f36134db47d7fd1655305".into();
        c
    }

    /// Real mainnet log captured via HyperSync (tests/fixtures/mainnet_logs.json).
    pub fn log(name: &str) -> Option<Event> {
        let fx: Value =
            serde_json::from_str(include_str!("../tests/fixtures/mainnet_logs.json")).unwrap();
        let l = &fx[name];
        let h = |v: &Value| hex::decode(v.as_str().unwrap()).unwrap();
        let topics: Vec<B256> = ["topic0", "topic1", "topic2", "topic3"]
            .iter()
            .filter(|k| !l[**k].is_null())
            .map(|k| B256::from_slice(&h(&l[*k])))
            .collect();
        decode(
            &cfg(),
            &Address::from_slice(&h(&l["address"])),
            &topics,
            &h(&l["data"]),
        )
        .unwrap()
    }

    fn v(
        venue: &str,
        kind: &'static str,
        account: &str,
        asset: Option<&str>,
        amount: &str,
        shares: Option<&str>,
        extra: Option<Value>,
    ) -> Option<Event> {
        Some(Event::Venue(Venue {
            venue: venue.into(),
            kind,
            account: account.into(),
            asset: asset.map(Into::into),
            amount: Some(amount.into()),
            shares: shares.map(Into::into),
            extra,
        }))
    }

    #[test]
    fn kuru() {
        // block 109194076: taker buy walking asks
        assert_eq!(
            log("trade"),
            Some(Event::Trade {
                order_id: 114095175,
                maker: "0xdf9aac7069c324d100af421d75a76500a3c53e5a".into(),
                taker: "0x34ae4d081c0d46b23ebf5292c1be4e45f8a73fc5".into(),
                is_buy: true,
                price: "27049000000000000".into(),
                updated_size: "0".into(),
                filled_size: "2000000000000".into(),
            })
        );
        assert_eq!(
            log("order_created"),
            Some(Event::OrderCreated {
                order_id: 114094278,
                owner: "0x34ae4d081c0d46b23ebf5292c1be4e45f8a73fc5".into(),
                is_buy: true,
                price: "2704100".into(),
                size: "554370000000000".into(),
            })
        );
        assert_eq!(
            log("orders_canceled"),
            Some(Event::OrdersCanceled {
                owner: "0x34ae4d081c0d46b23ebf5292c1be4e45f8a73fc5".into(),
                ids: vec![114094278],
            })
        );
    }

    #[test]
    fn lending_and_vaults() {
        let usdt0 = "0xe7cd86e13ac4309349f30b3435a9d337750fc82d";
        let usdc = "0x754704bc059f8c67012fed69bc8a327a5aafb603";
        let mon = "0x0000000000000000000000000000000000000000";
        assert_eq!(
            log("aave_supply"),
            v(
                "aave",
                "supply",
                "0x5e7109a755fd2b86361f95065cde07fbab50ca1d",
                Some(usdt0),
                "22346608",
                None,
                None
            )
        );
        assert_eq!(
            log("aave_withdraw"),
            v(
                "aave",
                "withdraw",
                "0x5cf91569ccfea5718645c526d3e5d513683818b0",
                Some(usdc),
                "59950000000",
                None,
                None
            )
        );
        assert_eq!(
            log("morpho_deposit"),
            v(
                "morpho",
                "deposit",
                "0x38f23831c4ef39272d458a089e932895a71e8e45",
                Some(usdc),
                "789905",
                Some("759932634144268785"),
                None
            )
        );
        assert_eq!(
            log("shmon_deposit"),
            v(
                "shmonad",
                "deposit",
                "0x0a56b9692b417684c5d7307ba97441e7bfb1957d",
                Some(mon),
                "3580000000000000000000",
                Some("2203722104235639199099"),
                None
            )
        );
        assert_eq!(
            log("shmon_withdraw"),
            v(
                "shmonad",
                "withdraw",
                "0xf8d76bdf4634fd80ff7433aa8dca117a7912436d",
                Some(mon),
                "107794688732138698846",
                Some("66862101524863535115"),
                None
            )
        );
        assert_eq!(
            log("activevault_deposit"),
            v(
                "activevault",
                "deposit",
                "0x78e0a7d840672faaa575fb8811310bbdb3a21f2f",
                None,
                "110000000000000000000",
                Some("240492703827646"),
                Some(json!({"quote": "2914524"}))
            )
        );
        assert_eq!(
            log("activevault_withdraw"),
            v(
                "activevault",
                "withdraw",
                "0x159c32afaebd2e99c95294458032d8a113c5c7fe",
                None,
                "227513515058831229985",
                Some("687796807612684"),
                Some(json!({"quote": "10235206"}))
            )
        );
        // treasury set to this transfer's recipient in cfg()
        assert_eq!(
            log("usdc_transfer"),
            v(
                "treasury",
                "fee",
                "0x5fabe7eb6629058bcaa0d2041cacd32638a7480b",
                Some(usdc),
                "361107",
                None,
                None
            )
        );
    }

    #[test]
    fn perpl() {
        assert_eq!(
            log("perpl_account_created"),
            Some(Event::PerplAccount {
                wallet: "0x6fe7f3bc9a5f94a0a4bb3513ce23c8a2a17fc367".into(),
                id: "5304".into()
            })
        );
        assert_eq!(
            log("perpl_collateral_deposit"),
            v(
                "perpl",
                "collateral_deposit",
                "543",
                None,
                "64850000",
                None,
                None
            )
        );
        assert_eq!(
            log("perpl_collateral_withdrawal"),
            v(
                "perpl",
                "collateral_withdraw",
                "2197",
                None,
                "2470000",
                None,
                None
            )
        );
        let p = |kind, acct: &str, id: &str, lot: &str, extra| {
            v(
                "perpl",
                kind,
                acct,
                Some(&format!("perp:{id}")),
                lot,
                None,
                Some(extra),
            )
        };
        assert_eq!(
            log("perpl_position_opened_v2"),
            p(
                "position_open",
                "2192",
                "1",
                "1",
                json!({"positionType": 1, "price": "833196", "deposit": "833237", "leverageHdths": "100"})
            )
        );
        assert_eq!(
            log("perpl_position_increased_v2"),
            p(
                "position_increase",
                "25",
                "31",
                "244041",
                json!({"positionType": 1, "price": "120431", "startLot": "244023", "deposit": "9796693483"})
            )
        );
        assert_eq!(
            log("perpl_position_decreased"),
            p(
                "position_decrease",
                "4608",
                "1",
                "2352",
                json!({"positionType": 0, "startLot": "2425", "deposit": "244977853", "pnl": "-4526", "funding": "0"})
            )
        );
        assert_eq!(
            log("perpl_position_closed"),
            p(
                "position_close",
                "812",
                "1",
                "0",
                json!({"positionType": 1, "price": "833196", "pnl": "0", "funding": "0"})
            )
        );
    }
}
