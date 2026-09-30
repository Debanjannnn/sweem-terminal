use tokio_postgres::NoTls;

/// everything the executor re-checks before signing for a user.
#[derive(Debug, Clone, Default)]
pub struct Execution {
    pub status: String,
    pub steps: Option<String>,
    pub schedule_active: bool,
    pub wallet_id: Option<String>,
    pub wallet_address: Option<String>,
}

pub type Db = deadpool_postgres::Pool;

/// pooled; a dead connection is dropped on checkout and replaced.
pub fn pool(url: &str) -> Result<Db, String> {
    let pg: tokio_postgres::Config = url.parse().map_err(|e| format!("DATABASE_URL: {e}"))?;
    let mgr = deadpool_postgres::Manager::from_config(
        pg,
        NoTls,
        deadpool_postgres::ManagerConfig { recycling_method: deadpool_postgres::RecyclingMethod::Fast },
    );
    deadpool_postgres::Pool::builder(mgr).max_size(8).build().map_err(|e| e.to_string())
}

async fn conn(db: &Db) -> Result<deadpool_postgres::Object, String> {
    db.get().await.map_err(|e| format!("postgres: {e}"))
}

pub async fn load(db: &Db, id: &str) -> Result<Option<Execution>, String> {
    let row = conn(db)
        .await?
        .query_opt(
            // the scheduler deactivates a stop-loss when it claims the fire, so its one execution still counts as active
            "select e.status, e.steps::text, coalesce(s.active or s.kind = 'stop_loss', true), d.privy_wallet_id, d.wallet_address
             from executions e
             left join schedules s on s.id = e.schedule_id
             left join delegations d on d.user_id = coalesce(e.user_id, s.user_id) and d.active
             where e.id = $1::text::uuid",
            &[&id],
        )
        .await
        .map_err(|e| e.to_string())?;
    Ok(row.map(|r| Execution { status: r.get(0), steps: r.get(1), schedule_active: r.get(2), wallet_id: r.get(3), wallet_address: r.get(4) }))
}

/// conditional transition; false = the row was not in `from` (a replay or a race lost).
pub async fn transition(db: &Db, id: &str, from: &str, to: &str, error: Option<&str>) -> Result<bool, String> {
    let n = conn(db)
        .await?
        .execute(
            "update executions set status = $3, error = coalesce($4, error), updated_at = now()
             where id = $1::text::uuid and status = $2",
            &[&id, &from, &to, &error],
        )
        .await
        .map_err(|e| e.to_string())?;
    Ok(n == 1)
}

/// one step confirmed: its hash appended, step_done advanced, tx_hash = latest.
pub async fn step_done(db: &Db, id: &str, hash: &str) -> Result<(), String> {
    conn(db)
        .await?
        .execute(
            "update executions set step_done = step_done + 1, tx_hashes = array_append(tx_hashes, $2), tx_hash = $2, updated_at = now()
             where id = $1::text::uuid and status = 'submitted'",
            &[&id, &hash],
        )
        .await
        .map(|_| ())
        .map_err(|e| e.to_string())
}

/// a restart abandons in-flight plans: fail them; step_done/tx_hashes keep what confirmed.
// ponytail: the step in flight at the crash is not re-watched; the caller reconciles from chain if it matters
pub async fn fail_interrupted(db: &Db) -> Result<u64, String> {
    conn(db)
        .await?
        .execute("update executions set status = 'failed', error = 'interrupted', updated_at = now() where status = 'submitted'", &[])
        .await
        .map_err(|e| e.to_string())
}
