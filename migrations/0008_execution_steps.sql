ALTER TABLE executions ALTER COLUMN schedule_id DROP NOT NULL;
ALTER TABLE executions ADD COLUMN IF NOT EXISTS user_id text REFERENCES users(id);
ALTER TABLE executions ADD COLUMN IF NOT EXISTS steps jsonb;
ALTER TABLE executions ADD COLUMN IF NOT EXISTS step_done int NOT NULL DEFAULT 0;
ALTER TABLE executions ADD COLUMN IF NOT EXISTS tx_hashes text[] NOT NULL DEFAULT '{}';
ALTER TABLE executions DROP CONSTRAINT IF EXISTS executions_owner;
ALTER TABLE executions ADD CONSTRAINT executions_owner CHECK (schedule_id IS NOT NULL OR user_id IS NOT NULL);
ALTER TABLE executions DROP CONSTRAINT IF EXISTS executions_kind_check;
