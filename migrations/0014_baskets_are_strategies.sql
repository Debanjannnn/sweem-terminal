-- Baskets are strategies now: the spot-basket feature (basket_defs, basket buy/sell, basket schedules) is gone.
-- Schedules target a market, a position (TP/SL, rebalance) or a strategy (recurring buy = deploy a new position per run).
-- Old basket / pre-target schedules keep their execution history: deactivated, target_type null (hidden from the api).
UPDATE schedules SET active = false, target_type = NULL WHERE target_type = 'basket' OR target_type IS NULL;
ALTER TABLE schedules DROP CONSTRAINT IF EXISTS schedules_target_type_check;
ALTER TABLE schedules ADD CONSTRAINT schedules_target_type_check CHECK (target_type IN ('market', 'position', 'strategy'));
ALTER TABLE executions DROP COLUMN IF EXISTS basket_id;
ALTER TABLE trade_prepares DROP COLUMN IF EXISTS basket_id;
DROP TABLE IF EXISTS basket_defs;
