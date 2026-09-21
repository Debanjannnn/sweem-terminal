-- oracle service. one schema per service; nothing outside `oracle` is touched.
CREATE SCHEMA IF NOT EXISTS oracle;

-- raw underlying samples. NUMERIC(78,0) holds a full uint256, WAD-scaled.
-- never store prices as floats: these feed the index positions settle against.
CREATE TABLE IF NOT EXISTS oracle.price_samples (
  id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  source       TEXT          NOT NULL,        -- 'pyth' | 'chainlink'
  symbol       TEXT          NOT NULL,        -- 'ETH/USD'
  price        NUMERIC(78,0) NOT NULL,        -- WAD
  source_ts    TIMESTAMPTZ   NOT NULL,        -- publishTime/updatedAt, not our clock
  observed_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),
  round_id     NUMERIC(78,0),                 -- chainlink only
  CONSTRAINT price_positive CHECK (price > 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS price_samples_uniq
  ON oracle.price_samples (symbol, source, source_ts);
CREATE INDEX IF NOT EXISTS price_samples_symbol_ts
  ON oracle.price_samples (symbol, source_ts DESC);

-- every publication attempt, including the ones the breaker refused. the refusals
-- are the audit trail for a manipulation attempt, so they are rows, not log lines.
CREATE TABLE IF NOT EXISTS oracle.index_publications (
  id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  market_id     TEXT          NOT NULL,
  kind          TEXT          NOT NULL,       -- 'vol' | 'power'
  value         NUMERIC(78,0) NOT NULL,       -- WAD
  previous      NUMERIC(78,0),                -- WAD, null on first publication
  deviation     NUMERIC(78,0) NOT NULL,       -- WAD
  status        TEXT          NOT NULL,
  reason        TEXT          NOT NULL,
  round_id      BIGINT,
  tx_hash       TEXT,
  created_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
  CONSTRAINT status_valid CHECK (status IN ('published','rejected','failed'))
);

CREATE INDEX IF NOT EXISTS index_publications_market_ts
  ON oracle.index_publications (market_id, created_at DESC);

-- drives the breaker baseline and the heartbeat, so the lookup must be cheap
CREATE INDEX IF NOT EXISTS index_publications_published
  ON oracle.index_publications (market_id, created_at DESC)
  WHERE status = 'published';
