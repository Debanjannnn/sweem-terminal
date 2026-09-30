use std::{collections::HashMap, sync::Arc};
use tokio::sync::{Mutex, OwnedMutexGuard};

/// privy assigns user nonces, so two in-flight sends from one wallet collide; a lease serializes them.
// ponytail: in-process only, move to postgres advisory locks if the executor ever runs replicated
#[derive(Default)]
pub struct Leases(Mutex<HashMap<String, Arc<Mutex<()>>>>);

impl Leases {
    pub async fn lease(&self, key: &str) -> OwnedMutexGuard<()> {
        let lock = self.0.lock().await.entry(key.to_string()).or_default().clone();
        lock.lock_owned().await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::Duration;

    #[tokio::test]
    async fn same_key_serializes_other_keys_do_not() {
        let l = Leases::default();
        let _a = l.lease("w1").await;
        assert!(tokio::time::timeout(Duration::from_millis(30), l.lease("w1")).await.is_err());
        assert!(tokio::time::timeout(Duration::from_millis(30), l.lease("w2")).await.is_ok());
    }
}
