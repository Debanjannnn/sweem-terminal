-- reconciler owns chain truth. every money/size column is numeric(78,0):
-- wad integers, never floats.

-- a snapshot is what `latest` said at a block. it is NEVER treated as settled:
-- an identical retry against the same height can return a different answer.
-- settlement only comes from a finalized read, in divergences/executions.
create table if not exists position_snapshots (
  market_id     text          not null,
  owner         text          not null,
  block_number  bigint        not null,
  size          numeric(78,0) not null,
  collateral    numeric(78,0) not null,
  entry_index   numeric(78,0) not null default 0,
  notional      numeric(78,0) not null default 0,
  margin_ratio  numeric(78,0) not null default 0,
  tier          text          not null default 'latest',
  created_at    timestamptz   not null default now(),
  primary key (market_id, owner, block_number)
);
create index if not exists position_snapshots_owner_idx on position_snapshots (owner, block_number desc);

create table if not exists risk_alerts (
  idempotency_key text        primary key,
  kind            text        not null,
  market_id       text        not null,
  owner           text        not null default '',
  block_number    bigint      not null default 0,
  detail          text        not null,
  created_at      timestamptz not null default now()
);
create index if not exists risk_alerts_kind_idx on risk_alerts (kind, created_at desc);

create table if not exists divergences (
  id                bigserial primary key,
  kind              text        not null,
  market_id         text        not null default '',
  owner             text        not null default '',
  expected          text        not null,
  actual            text        not null,
  reason            text        not null,
  observed_at_block bigint      not null,
  created_at        timestamptz not null default now()
);
create index if not exists divergences_kind_idx on divergences (kind, created_at desc);

create table if not exists funding_snapshots (
  market_id    text          not null,
  observed_at  bigint        not null,
  funding_rate numeric(78,0) not null,
  created_at   timestamptz   not null default now(),
  primary key (market_id, observed_at)
);

create table if not exists oi_snapshots (
  market_id      text          not null,
  block_number   bigint        not null,
  long_oi        numeric(78,0) not null,
  short_oi       numeric(78,0) not null,
  max_oi_per_side numeric(78,0) not null default 0,
  created_at     timestamptz   not null default now(),
  primary key (market_id, block_number)
);
