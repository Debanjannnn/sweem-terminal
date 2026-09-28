use tokio_postgres::NoTls;

/// everything the executor re-checks before signing for a user.
#[derive(Debug, Clone, Default)]
pub struct Execution {
    pub status: String,
    pub kind: String,
    pub amount: Option<String>,
    pub min_out: Option<String>,
    pub schedule_active: bool,
    pub basket: String,
    pub within_cap: bool,
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

/// submitted rows with a hash: receipts to (re)watch after a restart.
pub async fn submitted(db: &Db) -> Result<Vec<(String, String)>, String> {
    let rows = conn(db)
        .await?
        .query("select id::text, tx_hash from executions where status = 'submitted' and tx_hash is not null", &[])
        .await
        .map_err(|e| e.to_string())?;
    Ok(rows.iter().map(|r| (r.get(0), r.get(1))).collect())
}

pub async fn load(db: &Db, id: &str) -> Result<Option<Execution>, String> {
    let row = conn(db)
            .await?
        .query_opt(
            "select e.status, e.kind, e.amount::text, e.min_out::text, s.active, s.basket,
                    coalesce(e.amount <= s.max_per_run_usdc * 1000000, false),
                    d.privy_wallet_id, d.wallet_address
             from executions e
             join schedules s on s.id = e.schedule_id
             left join delegations d on d.user_id = s.user_id and d.active
             where e.id = $1::text::uuid",
            &[&id],
        )
        .await
        .map_err(|e| e.to_string())?;
    Ok(row.map(|r| Execution {
        status: r.get(0),
        kind: r.get(1),
        amount: r.get(2),
        min_out: r.get(3),
        schedule_active: r.get(4),
        basket: r.get(5),
        within_cap: r.get(6),
        wallet_id: r.get(7),
        wallet_address: r.get(8),
    }))
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

pub async fn set_tx(db: &Db, id: &str, hash: &str) -> Result<(), String> {
    conn(db).await?.execute("update executions set tx_hash = $2, updated_at = now() where id = $1::text::uuid", &[&id, &hash])
        .await
        .map(|_| ())
        .map_err(|e| e.to_string())
}
