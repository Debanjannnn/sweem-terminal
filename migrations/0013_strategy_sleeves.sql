-- multi-token strategies: planner.Sleeve[] (asset 'MULTI', sources '[]'); null = single-asset row
ALTER TABLE strategies ADD COLUMN IF NOT EXISTS sleeves jsonb;
