ALTER TABLE kuru_trades ADD COLUMN IF NOT EXISTS idx int NOT NULL DEFAULT 0;
ALTER TABLE kuru_trades DROP CONSTRAINT IF EXISTS kuru_trades_pkey;
ALTER TABLE kuru_trades ADD PRIMARY KEY (tx_hash, log_index, idx);
