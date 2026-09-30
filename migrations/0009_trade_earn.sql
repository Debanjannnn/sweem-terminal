-- off-chain basket definitions; the old indexer `baskets` (ERC-20) table is retired but left in place
CREATE TABLE IF NOT EXISTS basket_defs (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  owner      text NOT NULL REFERENCES users(id),
  name       text NOT NULL,
  legs       jsonb NOT NULL, -- [{market, weightBps}]
  public     boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS basket_defs_owner ON basket_defs(owner);

CREATE TABLE IF NOT EXISTS strategies (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  creator    text NOT NULL REFERENCES users(id),
  name       text NOT NULL,
  asset      text NOT NULL,
  sources    jsonb NOT NULL, -- [{kind, venue, weightBps, params}]
  rules      jsonb NOT NULL DEFAULT '{}',
  public     boolean NOT NULL DEFAULT true,
  backtest   jsonb, -- last backtest summary
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS strategies_creator ON strategies(creator);

CREATE TABLE IF NOT EXISTS strategy_positions (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  strategy_id       uuid NOT NULL REFERENCES strategies(id),
  user_id           text NOT NULL REFERENCES users(id),
  wallet            text NOT NULL,
  principal_usdc    numeric NOT NULL, -- human USDC deployed incl. fee
  status            text NOT NULL DEFAULT 'opening' CHECK (status IN ('opening', 'open', 'closing', 'closed', 'failed')),
  legs              jsonb NOT NULL DEFAULT '[]', -- planner.LegState per source
  execution_id      uuid,
  opened_at         timestamptz NOT NULL DEFAULT now(),
  closed_at         timestamptz,
  last_rebalance_at timestamptz
);
CREATE INDEX IF NOT EXISTS strategy_positions_user ON strategy_positions(user_id);

-- fee_usdc / creator_owed_usdc human USDC; creator null for own strategies
CREATE TABLE IF NOT EXISTS fee_ledger (
  execution_id      uuid PRIMARY KEY REFERENCES executions(id),
  user_id           text NOT NULL REFERENCES users(id),
  strategy_id       uuid NOT NULL REFERENCES strategies(id),
  creator           text,
  fee_usdc          numeric NOT NULL,
  creator_owed_usdc numeric NOT NULL DEFAULT 0,
  paid              boolean NOT NULL DEFAULT false,
  created_at        timestamptz NOT NULL DEFAULT now()
);

-- limit orders; price/size raw in the market's pricePrecision/sizePrecision
CREATE TABLE IF NOT EXISTS orders (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id       text NOT NULL REFERENCES users(id),
  market        text NOT NULL,
  kuru_order_id text,
  side          text NOT NULL CHECK (side IN ('buy', 'sell')),
  price         numeric NOT NULL,
  size          numeric NOT NULL,
  status        text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'open', 'cancelled', 'failed')),
  execution_id  uuid REFERENCES executions(id),
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS orders_user ON orders(user_id);

ALTER TABLE executions ADD COLUMN IF NOT EXISTS basket_id uuid;
ALTER TABLE executions ADD COLUMN IF NOT EXISTS position_id uuid;
CREATE INDEX IF NOT EXISTS executions_user ON executions(user_id, created_at);

ALTER TABLE schedules ALTER COLUMN basket DROP NOT NULL;
ALTER TABLE schedules DROP CONSTRAINT IF EXISTS schedules_kind_check;
ALTER TABLE schedules ADD CONSTRAINT schedules_kind_check CHECK (kind IN ('recurring', 'stop_loss', 'dca', 'take_profit', 'rebalance'));
ALTER TABLE schedules ADD COLUMN IF NOT EXISTS target_type text CHECK (target_type IN ('market', 'basket', 'position'));
ALTER TABLE schedules ADD COLUMN IF NOT EXISTS target_id text;
ALTER TABLE schedules ADD COLUMN IF NOT EXISTS trigger_price numeric;
ALTER TABLE schedules ADD COLUMN IF NOT EXISTS sell_bps int CHECK (sell_bps BETWEEN 1 AND 10000);
ALTER TABLE schedules ADD COLUMN IF NOT EXISTS drift_bps int CHECK (drift_bps > 0);
-- legacy stop_loss rows need stop_nav_per_share; new ones use trigger_price
ALTER TABLE schedules DROP CONSTRAINT IF EXISTS schedules_check1;
ALTER TABLE schedules ADD CONSTRAINT schedules_check1 CHECK (kind <> 'stop_loss' OR (stop_nav_per_share > 0 AND redeem_bps IS NOT NULL) OR (trigger_price > 0 AND sell_bps IS NOT NULL));

-- planned strategy_positions.legs, applied to the position when the execution confirms (scheduler reconcile)
ALTER TABLE executions ADD COLUMN IF NOT EXISTS legs jsonb;
