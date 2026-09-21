use std::{collections::HashMap, sync::Arc};

use tokio::sync::{Mutex, OwnedMutexGuard};

/// privy assigns the nonce, so two in-flight sends from one wallet can collide
/// on it. a lease serializes them; nothing else is ordered.
// ponytail: in-process only, move to redis if the executor ever runs replicated
#[derive(Default)]
pub struct Nonces(Mutex<HashMap<String, Arc<Mutex<()>>>>);

impl Nonces {
    pub async fn lease(&self, wallet_id: &str) -> OwnedMutexGuard<()> {
        let lock = self
            .0
            .lock()
            .await
            .entry(wallet_id.to_string())
            .or_default()
            .clone();
        lock.lock_owned().await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicUsize, Ordering};

    #[tokio::test]
    async fn same_wallet_serializes() {
        let n = Arc::new(Nonces::default());
        let live = Arc::new(AtomicUsize::new(0));
        let peak = Arc::new(AtomicUsize::new(0));

        let tasks: Vec<_> = (0..8)
            .map(|_| {
                let (n, live, peak) = (n.clone(), live.clone(), peak.clone());
                tokio::spawn(async move {
                    let _g = n.lease("w1").await;
                    let c = live.fetch_add(1, Ordering::SeqCst) + 1;
                    peak.fetch_max(c, Ordering::SeqCst);
                    tokio::time::sleep(std::time::Duration::from_millis(2)).await;
                    live.fetch_sub(1, Ordering::SeqCst);
                })
            })
            .collect();
        for t in tasks {
            t.await.unwrap();
        }
        assert_eq!(peak.load(Ordering::SeqCst), 1);
    }

    #[tokio::test]
    async fn different_wallets_do_not_block_each_other() {
        let n = Nonces::default();
        let _a = n.lease("w1").await;
        let _b = tokio::time::timeout(std::time::Duration::from_millis(50), n.lease("w2"))
            .await
            .expect("a second wallet must not wait on the first");
    }
}
