use alloy_primitives::{hex, Address, B256};
use alloy_sol_types::{sol, SolEvent};
use anyhow::{ensure, Result};
use serde_json::{json, Value};

sol!(Factory, "../../abi/SweemBasketFactory.json");
sol!(Basket, "../../abi/SweemBasket.json");
sol!(
    #[allow(clippy::too_many_arguments)]
    OrderBook,
    "../../abi/kuru/OrderBook.json"
);

use Basket::{Minted, Redeemed};
use Factory::{BasketCreated, MarketAllowed};
use OrderBook::TradesPacked;

pub const TOPICS_FACTORY: [B256; 2] =
    [MarketAllowed::SIGNATURE_HASH, BasketCreated::SIGNATURE_HASH];
pub const TOPICS_BASKET: [B256; 2] = [Minted::SIGNATURE_HASH, Redeemed::SIGNATURE_HASH];
pub const TOPIC_TRADES: B256 = TradesPacked::SIGNATURE_HASH;

#[derive(Debug, PartialEq)]
pub enum Event {
    MarketAllowed {
        market: String,
        allowed: bool,
    },
    BasketCreated {
        basket: String,
        name: String,
        symbol: String,
        components: Value,
    },
    Minted {
        basket: String,
        account: String,
        usdc: String,
        shares: String,
    },
    Redeemed {
        basket: String,
        account: String,
        shares: String,
        usdc: String,
    },
    Trades {
        market: String,
        trades: Vec<Trade>,
    },
}

#[derive(Debug, PartialEq)]
pub struct Trade {
    pub price: u32,
    pub size: u128,
    pub side: &'static str,
}

pub fn addr(a: impl AsRef<[u8]>) -> String {
    hex::encode_prefixed(a)
}

/// Decodes a log by topic0; the source query already restricts emitters, so address is only recorded.
pub fn decode(address: &Address, topics: &[B256], data: &[u8]) -> Result<Option<Event>> {
    let Some(t0) = topics.first() else {
        return Ok(None);
    };
    let at = addr(address);
    Ok(Some(match *t0 {
        MarketAllowed::SIGNATURE_HASH => {
            let e = MarketAllowed::decode_raw_log(topics.iter().copied(), data)?;
            Event::MarketAllowed {
                market: addr(e.market),
                allowed: e.allowed,
            }
        }
        BasketCreated::SIGNATURE_HASH => {
            let e = BasketCreated::decode_raw_log(topics.iter().copied(), data)?;
            let components = e
                .components
                .iter()
                .map(|c| json!({"market": addr(c.market), "weightBps": c.weightBps}))
                .collect();
            Event::BasketCreated {
                basket: addr(e.basket),
                name: e.name,
                symbol: e.symbol,
                components,
            }
        }
        Minted::SIGNATURE_HASH => {
            let e = Minted::decode_raw_log(topics.iter().copied(), data)?;
            Event::Minted {
                basket: at,
                account: addr(e.user),
                usdc: e.usdcIn.to_string(),
                shares: e.shares.to_string(),
            }
        }
        Redeemed::SIGNATURE_HASH => {
            let e = Redeemed::decode_raw_log(topics.iter().copied(), data)?;
            Event::Redeemed {
                basket: at,
                account: addr(e.user),
                shares: e.shares.to_string(),
                usdc: e.usdcOut.to_string(),
            }
        }
        TradesPacked::SIGNATURE_HASH => {
            let e = TradesPacked::decode_raw_log(topics.iter().copied(), data)?;
            Event::Trades {
                market: at,
                trades: unpack_trades(&e.packedTrades)?,
            }
        }
        _ => return Ok(None),
    }))
}

/// 64-byte records (tasks/kuru-spot-v2.md §10.1): userId(5) slot(1) flags(1) price(4) fillSize(12) orderId(8) pad(1) | word1.
pub fn unpack_trades(b: &[u8]) -> Result<Vec<Trade>> {
    ensure!(
        b.len().is_multiple_of(64),
        "packedTrades len {} not multiple of 64",
        b.len()
    );
    Ok(b.chunks(64)
        .map(|r| {
            let mut size = [0u8; 16];
            size[4..].copy_from_slice(&r[11..23]);
            Trade {
                price: u32::from_be_bytes(r[7..11].try_into().unwrap()),
                size: u128::from_be_bytes(size),
                // flags bit0 = maker bid, so the taker sold
                side: if r[6] & 1 == 1 { "sell" } else { "buy" },
            }
        })
        .collect())
}

#[cfg(test)]
mod tests {
    use super::*;
    use alloy_primitives::{address, U256};
    use alloy_sol_types::SolValue;

    fn topics(t: &[B256]) -> Vec<B256> {
        t.to_vec()
    }

    #[test]
    fn factory_events() {
        let m = address!("a9C2936656a7D2143720BcD91Ba8506200B7CbE7");
        let f = Address::ZERO;
        let e = decode(
            &f,
            &topics(&[MarketAllowed::SIGNATURE_HASH, m.into_word()]),
            &true.abi_encode(),
        )
        .unwrap();
        assert_eq!(
            e,
            Some(Event::MarketAllowed {
                market: "0xa9c2936656a7d2143720bcd91ba8506200b7cbe7".into(),
                allowed: true
            })
        );

        let b = address!("E0b3bAafa8EDD1db95b49e62ea0fB18c04353870");
        let ev = BasketCreated {
            basket: b,
            name: "Blue".into(),
            symbol: "BLUE".into(),
            components: vec![Factory::Component {
                market: m,
                weightBps: 10_000,
            }],
        };
        let log = ev.encode_log_data();
        let got = decode(&f, log.topics(), &log.data).unwrap().unwrap();
        assert_eq!(
            got,
            Event::BasketCreated {
                basket: "0xe0b3baafa8edd1db95b49e62ea0fb18c04353870".into(),
                name: "Blue".into(),
                symbol: "BLUE".into(),
                components: json!([{"market": "0xa9c2936656a7d2143720bcd91ba8506200b7cbe7", "weightBps": 10000}]),
            }
        );
    }

    #[test]
    fn basket_events() {
        let b = address!("E0b3bAafa8EDD1db95b49e62ea0fB18c04353870");
        let u = address!("df8f5E4d6e27ccfA85fD74Fc9b3c1c8632059970");
        let big = U256::from(10u64).pow(U256::from(30));
        let log = Minted {
            user: u,
            usdcIn: U256::from(5_000_000u64),
            shares: big,
        }
        .encode_log_data();
        assert_eq!(
            decode(&b, log.topics(), &log.data).unwrap().unwrap(),
            Event::Minted {
                basket: addr(b),
                account: addr(u),
                usdc: "5000000".into(),
                shares: big.to_string(),
            }
        );
        let log = Redeemed {
            user: u,
            shares: U256::from(7u64),
            usdcOut: U256::from(9u64),
        }
        .encode_log_data();
        assert_eq!(
            decode(&b, log.topics(), &log.data).unwrap().unwrap(),
            Event::Redeemed {
                basket: addr(b),
                account: addr(u),
                shares: "7".into(),
                usdc: "9".into()
            }
        );
    }

    #[test]
    fn trades_packed_real_logs() {
        let fx: Value =
            serde_json::from_str(include_str!("../tests/fixtures/trades_packed.json")).unwrap();
        let parse = |i: usize| {
            let l = &fx[i];
            let h = |k: &str| hex::decode(l[k].as_str().unwrap()).unwrap();
            let ts: Vec<B256> = ["topic0", "topic1", "topic2"]
                .iter()
                .map(|k| B256::from_slice(&h(k)))
                .collect();
            decode(&Address::from_slice(&h("address")), &ts, &h("data"))
                .unwrap()
                .unwrap()
        };
        let weth = "0xa9c2936656a7d2143720bcd91ba8506200b7cbe7".to_string();
        // block 64699095 log 112: two maker-bid fills of one taker sell, second has MATCH_END
        assert_eq!(
            parse(0),
            Event::Trades {
                market: weth.clone(),
                trades: vec![
                    Trade {
                        price: 274579,
                        size: 330000000,
                        side: "sell"
                    },
                    Trade {
                        price: 274318,
                        size: 170000000,
                        side: "sell"
                    },
                ],
            }
        );
        // block 64699421 log 20: values cross-checked in kuru-spot-v2.md §10.1
        assert_eq!(
            parse(1),
            Event::Trades {
                market: weth,
                trades: vec![Trade {
                    price: 274884,
                    size: 330000000,
                    side: "sell"
                }]
            }
        );
    }

    #[test]
    fn rejects_bad_packed_len() {
        assert!(unpack_trades(&[0u8; 63]).is_err());
    }
}
