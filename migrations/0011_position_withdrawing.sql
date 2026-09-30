-- a withdraw that fails stays 'withdrawing' until a resumed withdraw (or reconcile) finishes it; never back to 'open'
ALTER TABLE strategy_positions DROP CONSTRAINT IF EXISTS strategy_positions_status_check;
ALTER TABLE strategy_positions ADD CONSTRAINT strategy_positions_status_check
  CHECK (status IN ('opening', 'open', 'closing', 'withdrawing', 'closed', 'failed'));
