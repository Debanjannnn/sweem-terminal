-- OHLCV in display units; publisher writes live buckets, history backfills older ones. t = bucket start (UTC, epoch-aligned).
create table if not exists candles (
  market   text not null,
  interval text not null check (interval in ('1m', '5m', '1h', '1d')),
  t        timestamptz not null,
  o        numeric not null,
  h        numeric not null,
  l        numeric not null,
  c        numeric not null,
  v        numeric not null,
  q        numeric not null,
  n        int not null,
  primary key (market, interval, t)
);
