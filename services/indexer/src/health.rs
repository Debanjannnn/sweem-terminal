use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpListener;

const STALE_SECS: u64 = 60;

pub fn now() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_secs()
}

async fn ready(db_url: &str, last_ok: &AtomicU64) -> bool {
    if now().saturating_sub(last_ok.load(Ordering::Relaxed)) > STALE_SECS {
        return false;
    }
    match crate::store::connect(db_url).await {
        Ok(db) => db.simple_query("select 1").await.is_ok(),
        Err(_) => false,
    }
}

/// `/health` liveness, `/healthz` = Postgres reachable + a batch succeeded within STALE_SECS.
pub async fn serve(port: u16, db_url: String, last_ok: Arc<AtomicU64>) -> anyhow::Result<()> {
    let l = TcpListener::bind(("0.0.0.0", port)).await?;
    loop {
        let (mut s, _) = l.accept().await?;
        let (db_url, last_ok) = (db_url.clone(), last_ok.clone());
        tokio::spawn(async move {
            let mut buf = [0u8; 1024];
            let n = s.read(&mut buf).await.unwrap_or(0);
            let req = String::from_utf8_lossy(&buf[..n]);
            let status = match req.split_whitespace().nth(1).unwrap_or("") {
                "/health" => "200 OK",
                "/healthz" if ready(&db_url, &last_ok).await => "200 OK",
                "/healthz" => "503 Service Unavailable",
                _ => "404 Not Found",
            };
            let body = format!("{{\"status\":\"{status}\"}}");
            let resp = format!(
                "HTTP/1.1 {status}\r\ncontent-type: application/json\r\ncontent-length: {}\r\nconnection: close\r\n\r\n{body}",
                body.len()
            );
            let _ = s.write_all(resp.as_bytes()).await;
        });
    }
}
