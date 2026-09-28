ALTER TABLE executions ADD COLUMN IF NOT EXISTS amount numeric;
ALTER TABLE executions ADD COLUMN IF NOT EXISTS min_out numeric;
