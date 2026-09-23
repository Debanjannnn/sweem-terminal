use alloy_primitives::{Address, Bytes, U256};
use serde::{Deserialize, Deserializer, Serialize, Serializer};
use std::collections::HashMap;

pub mod account;
pub mod kuru;

pub const DEFAULT_SLIPPAGE_BPS: u32 = 50;
pub const MIN_SLIPPAGE_BPS: u32 = 10;
pub const MAX_SLIPPAGE_BPS: u32 = 300;

#[derive(Clone, Copy, Debug, PartialEq, Eq, Deserialize, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Venue {
    Kuru,
}

impl Venue {
    pub fn parse(s: &str) -> Option<Self> {
        match s.trim().to_ascii_lowercase().as_str() {
            "kuru" => Some(Venue::Kuru),
            _ => None,
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Side {
    Buy,
    Sell,
}

impl Side {
    pub fn parse(s: &str) -> Option<Self> {
        match s.trim().to_ascii_lowercase().as_str() {
            "buy" => Some(Side::Buy),
            "sell" => Some(Side::Sell),
            _ => None,
        }
    }

    pub fn is_buy(self) -> bool {
        self == Side::Buy
    }
}

/// a caller may only narrow its loss bound, never widen it.
pub fn clamp_slippage(bps: u32) -> u32 {
    if bps == 0 {
        return DEFAULT_SLIPPAGE_BPS;
    }
    bps.clamp(MIN_SLIPPAGE_BPS, MAX_SLIPPAGE_BPS)
}

// venue integers are decimal strings on the wire: json numbers lose precision above 2^53
mod dec {
    use super::*;

    pub fn serialize<S: Serializer>(v: &U256, s: S) -> Result<S::Ok, S::Error> {
        s.serialize_str(&v.to_string())
    }

    pub fn deserialize<'de, D: Deserializer<'de>>(d: D) -> Result<U256, D::Error> {
        let s = String::deserialize(d)?;
        U256::from_str_radix(s.trim(), 10).map_err(serde::de::Error::custom)
    }
}

/// every scale in play for one market, all read from chain (see kuru-markets.json).
#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct Market {
    pub id: String,
    pub chain_id: u64,
    pub market_address: Address,
    pub base_token: Address,
    pub quote_token: Address,
    pub base_decimals: u8,
    pub quote_decimals: u8,
    #[serde(with = "dec")]
    pub price_precision: U256,
    #[serde(with = "dec")]
    pub size_precision: U256,
    /// book quantity -> raw base atoms. == 10^base_decimals / size_precision
    #[serde(with = "dec")]
    pub base_size_multiplier: U256,
    #[serde(with = "dec")]
    pub tick_size: U256,
    /// raw quote atoms, not the x18 form the data api reports
    #[serde(with = "dec")]
    pub min_quote_notional: U256,
    #[serde(with = "dec")]
    pub max_quote_notional: U256,
    #[serde(with = "dec")]
    pub taker_fee_pps: U256,
    #[serde(with = "dec")]
    pub maker_fee_pps: U256,
}

impl Market {
    pub fn base_is_native(&self) -> bool {
        self.base_token.is_zero()
    }

    pub fn token(&self, which: &str) -> Option<Address> {
        match which.trim().to_ascii_lowercase().as_str() {
            "base" => Some(self.base_token),
            "quote" => Some(self.quote_token),
            _ => None,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Call {
    pub to: Address,
    pub value: U256,
    pub data: Bytes,
}

/// the allowlist. every address the executor can ever call comes from here.
#[derive(Clone, Debug, Default)]
pub struct Registry(HashMap<String, Market>);

// "mon-usdc", "MON/USDC" and "MONUSDC" are the same committed entry
fn norm(id: &str) -> String {
    id.chars().filter(|c| c.is_ascii_alphanumeric()).collect::<String>().to_ascii_uppercase()
}

impl Registry {
    pub fn load(path: &str) -> Result<Self, String> {
        let body = std::fs::read_to_string(path).map_err(|e| format!("read markets {path}: {e}"))?;
        Self::parse(&body).map_err(|e| format!("markets {path}: {e}"))
    }

    pub fn parse(body: &str) -> Result<Self, String> {
        let markets: Vec<Market> = serde_json::from_str(body).map_err(|e| e.to_string())?;
        if markets.is_empty() {
            return Err("allowlist is empty: nothing would be routable".into());
        }
        let mut map = HashMap::new();
        for m in markets {
            if !crate::config::CHAIN_IDS.contains(&m.chain_id) {
                return Err(format!("market {}: unsupported chain_id {}", m.id, m.chain_id));
            }
            if m.market_address.is_zero() || m.quote_token.is_zero() {
                return Err(format!("market {}: market and quote addresses must be set", m.id));
            }
            if m.base_token == m.quote_token {
                return Err(format!("market {}: base and quote are the same token", m.id));
            }
            if m.base_decimals > 18 || m.quote_decimals > 18 {
                return Err(format!("market {}: implausible decimals", m.id));
            }
            if m.price_precision.is_zero() || m.size_precision.is_zero() || m.tick_size.is_zero() {
                return Err(format!("market {}: precisions must be non-zero", m.id));
            }
            // a wrong multiplier is silent: sizes would be off by a power of ten
            let want = U256::from(10u64).pow(U256::from(m.base_decimals));
            if m.base_size_multiplier * m.size_precision != want {
                return Err(format!(
                    "market {}: base_size_multiplier {} * size_precision {} != 10^{}",
                    m.id, m.base_size_multiplier, m.size_precision, m.base_decimals
                ));
            }
            if map.insert(norm(&m.id), m.clone()).is_some() {
                return Err(format!("market {}: duplicate id", m.id));
            }
        }
        Ok(Self(map))
    }

    pub fn get(&self, id: &str) -> Option<&Market> {
        self.0.get(&norm(id))
    }

    pub fn all(&self) -> Vec<&Market> {
        let mut out: Vec<&Market> = self.0.values().collect();
        out.sort_by(|a, b| a.id.cmp(&b.id));
        out
    }

    pub fn len(&self) -> usize {
        self.0.len()
    }
}

#[cfg(test)]
pub(crate) fn test_registry() -> Registry {
    Registry::load("kuru-markets.json").expect("kuru-markets.json must parse")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn shipped_allowlist_is_valid_and_testnet() {
        let r = test_registry();
        assert!(r.len() >= 4, "expected the live testnet markets");
        for m in r.all() {
            assert_eq!(m.chain_id, 10143, "only testnet entries are committed");
            assert!(!m.market_address.is_zero());
            assert_ne!(m.market_address, m.quote_token);
            assert_eq!(m.taker_fee_pps, U256::from(7000u64));
            assert_eq!(m.maker_fee_pps, U256::from(4000u64));
            assert_eq!(m.min_quote_notional, U256::from(10_000_000u64), "raw quote atoms, not x18");
        }
        assert!(r.get("MON-USDC").is_some());
        assert!(r.get("XAUT0-USDC").is_some(), "the market symbol is XAUT0USDC");
    }

    /// the identity that makes every raw-atom conversion correct.
    #[test]
    fn base_size_multiplier_matches_decimals_on_every_market() {
        for m in test_registry().all() {
            let want = U256::from(10u64).pow(U256::from(m.base_decimals));
            assert_eq!(
                m.base_size_multiplier * m.size_precision,
                want,
                "{}: {} * {} != 10^{}",
                m.id,
                m.base_size_multiplier,
                m.size_precision,
                m.base_decimals
            );
        }
    }

    #[test]
    fn a_wrong_multiplier_fails_the_load() {
        let entry = |patch: &str| {
            format!(
                r#"[{{"id":"A-B","chain_id":10143,
                 "market_address":"0x00000000000000000000000000000000000000aa",
                 "base_token":"0x00000000000000000000000000000000000000bb",
                 "quote_token":"0x00000000000000000000000000000000000000cc",
                 "base_decimals":18,"quote_decimals":6,"price_precision":"100",
                 "size_precision":"10000000000","base_size_multiplier":"100000000",
                 "tick_size":"1","min_quote_notional":"10000000",
                 "max_quote_notional":"5000000000000",
                 "taker_fee_pps":"7000","maker_fee_pps":"4000"{patch}}}]"#
            )
        };
        assert!(Registry::parse(&entry("")).is_ok());
        // the exact bug the old file shipped: multiplier missing / wrong by 1e8
        assert!(Registry::parse(&entry(r#","base_size_multiplier":"1""#)).is_err());
        assert!(Registry::parse(&entry(r#","chain_id":1"#)).is_err(), "wrong chain");
        assert!(
            Registry::parse(&entry(r#","market_address":"0x0000000000000000000000000000000000000000""#)).is_err()
        );
        assert!(
            Registry::parse(&entry(r#","base_token":"0x00000000000000000000000000000000000000cc""#)).is_err(),
            "base must differ from quote"
        );
        assert!(Registry::parse(&entry(r#","base_decimals":77"#)).is_err());
        assert!(Registry::parse(&entry(r#","price_precision":"1e2""#)).is_err(), "no scientific notation");
    }

    #[test]
    fn lookup_is_punctuation_and_case_insensitive() {
        let r = test_registry();
        let want = r.get("MON-USDC").unwrap().market_address;
        for id in ["mon-usdc", "MONUSDC", "MON/USDC", "mon_usdc"] {
            assert_eq!(r.get(id).unwrap().market_address, want, "{id}");
        }
        assert!(r.get("EVIL-USDC").is_none());
        assert!(r.get("0xdeadbeef").is_none());
    }

    #[test]
    fn empty_allowlist_fails_closed() {
        let err = Registry::parse("[]").unwrap_err();
        assert!(err.contains("empty"), "{err}");
        assert!(Registry::parse("{ not json").is_err());
        assert!(Registry::default().get("MON-USDC").is_none());
    }

    #[test]
    fn slippage_is_clamped_server_side() {
        assert_eq!(clamp_slippage(0), DEFAULT_SLIPPAGE_BPS);
        assert_eq!(clamp_slippage(1), MIN_SLIPPAGE_BPS);
        assert_eq!(clamp_slippage(100), 100);
        assert_eq!(clamp_slippage(10_000), MAX_SLIPPAGE_BPS, "a caller cannot widen its loss bound");
    }

    #[test]
    fn side_and_venue_parse_strictly() {
        assert_eq!(Side::parse("BUY"), Some(Side::Buy));
        assert_eq!(Side::parse(" sell "), Some(Side::Sell));
        assert_eq!(Side::parse("short"), None);
        assert_eq!(Venue::parse("Kuru"), Some(Venue::Kuru));
        assert_eq!(Venue::parse("uniswap"), None);
    }

    #[test]
    fn only_mon_is_native_and_tokens_resolve_by_name() {
        let r = test_registry();
        let mon = r.get("MON-USDC").unwrap();
        assert!(mon.base_is_native());
        assert!(!r.get("WETH-USDC").unwrap().base_is_native());
        assert_eq!(mon.token("quote"), Some(mon.quote_token));
        assert_eq!(mon.token("BASE"), Some(mon.base_token));
        assert_eq!(mon.token("0xdeadbeef"), None, "a token is named, never supplied");
    }
}
