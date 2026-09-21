use std::{
    collections::{HashMap, VecDeque},
    sync::Mutex,
    time::{SystemTime, UNIX_EPOCH},
};

use crate::route::RouteResponse;

const CAPACITY: usize = 10_000;

/// replays the recorded answer for an intent_key instead of signing twice.
/// concurrent duplicates are kept apart by the per-wallet nonce lease.
// ponytail: in-process and bounded, move to postgres when the executor replicates
#[derive(Default)]
pub struct Idem(Mutex<(HashMap<String, RouteResponse>, VecDeque<String>)>);

impl Idem {
    pub fn get(&self, intent_key: &str) -> Option<RouteResponse> {
        self.0.lock().ok()?.0.get(intent_key).cloned()
    }

    pub fn put(&self, intent_key: &str, resp: &RouteResponse) {
        let Ok(mut g) = self.0.lock() else { return };
        let (map, order) = &mut *g;
        if map.insert(intent_key.to_string(), resp.clone()).is_none() {
            order.push_back(intent_key.to_string());
        }
        while order.len() > CAPACITY {
            if let Some(old) = order.pop_front() {
                map.remove(&old);
            }
        }
    }

    pub fn len(&self) -> usize {
        self.0.lock().map(|g| g.0.len()).unwrap_or(0)
    }
}

/// header `privy-idempotency-key`: stable across retries of the same step.
pub fn privy_key(intent_key: &str, step: &str) -> String {
    format!("{intent_key}:{step}")
}

/// privy rejects a reused reference_id with a 400, so it carries a clock.
pub fn reference_id(intent_key: &str, step: &str) -> String {
    let nanos = SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_nanos()).unwrap_or(0);
    format!("{intent_key}:{step}:{nanos}")
}

#[cfg(test)]
mod tests {
    use super::*;

    fn resp(hash: &str) -> RouteResponse {
        RouteResponse { tx_hash: hash.into(), status: "submitted".into(), ..RouteResponse::default() }
    }

    #[test]
    fn replays_a_recorded_answer() {
        let i = Idem::default();
        assert!(i.get("k1").is_none());
        i.put("k1", &resp("0xaa"));
        assert_eq!(i.get("k1").unwrap().tx_hash, "0xaa");
        assert!(i.get("k2").is_none());
    }

    #[test]
    fn evicts_oldest_beyond_capacity() {
        let i = Idem::default();
        for n in 0..CAPACITY + 10 {
            i.put(&format!("k{n}"), &resp("0xaa"));
        }
        assert_eq!(i.len(), CAPACITY);
        assert!(i.get("k0").is_none(), "oldest must be evicted");
        assert!(i.get(&format!("k{}", CAPACITY + 9)).is_some());
    }

    #[test]
    fn idempotency_key_is_stable_and_reference_id_is_not() {
        assert_eq!(privy_key("i1", "swap"), privy_key("i1", "swap"));
        assert_ne!(privy_key("i1", "swap"), privy_key("i1", "approve"));
        assert_ne!(reference_id("i1", "swap"), reference_id("i1", "swap"));
    }
}
