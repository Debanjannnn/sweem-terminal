use alloy_primitives::{Address, Bytes, U256};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;

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
}

/// a caller may only narrow its loss bound, never widen it.
pub fn clamp_slippage(bps: u32) -> u32 {
    if bps == 0 {
        return DEFAULT_SLIPPAGE_BPS;
    }
    bps.clamp(MIN_SLIPPAGE_BPS, MAX_SLIPPAGE_BPS)
}

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct Market {
    pub id: String,
    pub chain_id: u64,
    pub market_address: Address,
    pub base_token: Address,
    pub quote_token: Address,
    pub base_decimals: u8,
    pub quote_decimals: u8,
    pub price_precision: String,
    pub size_precision: String,
    pub tick_size: String,
    pub min_size: String,
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
        }
        assert!(r.get("MON-USDC").is_some());
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
    fn validation_rejects_bad_entries() {
        let entry = |patch: &str| {
            format!(
                r#"[{{"id":"A-B","chain_id":10143,
                 "market_address":"0x00000000000000000000000000000000000000aa",
                 "base_token":"0x00000000000000000000000000000000000000bb",
                 "quote_token":"0x00000000000000000000000000000000000000cc",
                 "base_decimals":18,"quote_decimals":6,"price_precision":"1",
                 "size_precision":"1","tick_size":"1","min_size":"1"{patch}}}]"#
            )
        };
        assert!(Registry::parse(&entry("")).is_ok());
        assert!(Registry::parse(&entry(r#","chain_id":1"#)).is_err(), "wrong chain");
        assert!(
            Registry::parse(&entry(r#","market_address":"0x0000000000000000000000000000000000000000""#)).is_err()
        );
        assert!(
            Registry::parse(&entry(r#","base_token":"0x00000000000000000000000000000000000000cc""#)).is_err(),
            "base must differ from quote"
        );
        assert!(Registry::parse(&entry(r#","base_decimals":77"#)).is_err());
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
}
