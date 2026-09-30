-- client-signed trades: the plan the api rendered for the user's own wallet, until its tx hashes are recorded
CREATE TABLE IF NOT EXISTS trade_prepares (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      text NOT NULL REFERENCES users(id),
  wallet       text NOT NULL,
  kind         text NOT NULL,
  basket_id    uuid,
  steps        jsonb NOT NULL,
  txs          jsonb NOT NULL, -- planner.TxRequest[] as returned to the client
  expires_at   timestamptz NOT NULL,
  execution_id uuid REFERENCES executions(id),
  created_at   timestamptz NOT NULL DEFAULT now()
);

-- who signed an execution: executor (delegated) or client (Privy embedded wallet, recorded after the fact)
ALTER TABLE executions ADD COLUMN IF NOT EXISTS signer text NOT NULL DEFAULT 'executor' CHECK (signer IN ('executor', 'client'));
