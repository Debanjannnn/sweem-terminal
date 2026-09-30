-- daily lending/LST APY; apy and apy_base are fractions (0.05 = 5%), apy includes reward tokens
CREATE TABLE IF NOT EXISTS series_rate (
  source text NOT NULL,
  venue text NOT NULL,
  asset text NOT NULL,
  day date NOT NULL,
  apy numeric NOT NULL,
  apy_base numeric NOT NULL,
  PRIMARY KEY (source, venue, asset, day)
);

-- Perpl funding per interval; rate = fraction of notional paid by longs to shorts (api rate / 1e6)
CREATE TABLE IF NOT EXISTS series_funding (
  perp_id int NOT NULL,
  ts timestamptz NOT NULL,
  rate numeric NOT NULL,
  PRIMARY KEY (perp_id, ts)
);

CREATE OR REPLACE VIEW series_funding_daily AS
SELECT perp_id, (ts AT TIME ZONE 'UTC')::date AS day, sum(rate) AS rate, count(*) AS n
FROM series_funding GROUP BY 1, 2;

-- Kuru price history goes to `candles` (0007); this cursor tracks the HyperSync backfill
CREATE TABLE IF NOT EXISTS series_cursor (
  name text PRIMARY KEY,
  block bigint NOT NULL
);
