ALTER TABLE executions DROP CONSTRAINT IF EXISTS executions_kind_check;
ALTER TABLE executions ADD CONSTRAINT executions_kind_check CHECK (kind IN ('mint', 'redeem'));
