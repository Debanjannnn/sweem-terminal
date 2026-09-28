create table users (
  id             text primary key, -- privy did
  wallet_address text,
  created_at     timestamptz not null default now()
);

create table delegations (
  id              uuid primary key default gen_random_uuid(),
  user_id         text not null references users(id),
  privy_wallet_id text not null,
  wallet_address  text not null,
  key_quorum_id   text not null,
  active          boolean not null default true,
  created_at      timestamptz not null default now(),
  revoked_at      timestamptz
);
create unique index delegations_one_active on delegations(user_id) where active;

create table schedules (
  id                 uuid primary key default gen_random_uuid(),
  user_id            text not null references users(id),
  basket             text not null,
  kind               text not null check (kind in ('recurring', 'stop_loss')),
  amount_usdc        numeric,
  interval_seconds   bigint check (interval_seconds > 0),
  next_run_at        timestamptz,
  stop_nav_per_share numeric,
  redeem_bps         int check (redeem_bps between 1 and 10000),
  max_slippage_bps   int not null check (max_slippage_bps between 0 and 10000),
  max_per_run_usdc   numeric not null,
  active             boolean not null default true,
  created_at         timestamptz not null default now(),
  check (kind <> 'recurring' or (amount_usdc > 0 and interval_seconds is not null and next_run_at is not null)),
  check (kind <> 'stop_loss' or (stop_nav_per_share > 0 and redeem_bps is not null))
);
create index schedules_due on schedules(active, next_run_at);
create index schedules_user on schedules(user_id);

create table executions (
  id          uuid primary key default gen_random_uuid(),
  schedule_id uuid not null references schedules(id),
  slot        bigint not null,
  kind        text not null check (kind in ('recurring', 'stop_loss')),
  status      text not null default 'pending' check (status in ('pending', 'submitted', 'confirmed', 'failed', 'rejected')),
  tx_hash     text,
  error       text,
  created_at  timestamptz not null default now(),
  updated_at  timestamptz not null default now(),
  unique (schedule_id, slot)
);
create index executions_status on executions(status);

create table baskets (
  address       text primary key,
  name          text not null,
  symbol        text not null,
  components    jsonb not null,
  created_block bigint not null,
  created_tx    text not null
);

create table market_allowlist (
  market        text primary key,
  allowed       boolean not null,
  updated_block bigint not null
);

create table mints (
  tx_hash   text not null,
  log_index int not null,
  basket    text not null,
  account   text not null,
  usdc      numeric not null,
  shares    numeric not null,
  block     bigint not null,
  ts        timestamptz not null,
  primary key (tx_hash, log_index)
);
create index mints_basket on mints(basket);
create index mints_account on mints(account);

create table redeems (
  tx_hash   text not null,
  log_index int not null,
  basket    text not null,
  account   text not null,
  shares    numeric not null,
  usdc      numeric not null,
  block     bigint not null,
  ts        timestamptz not null,
  primary key (tx_hash, log_index)
);
create index redeems_basket on redeems(basket);
create index redeems_account on redeems(account);

create table kuru_trades (
  tx_hash   text not null,
  log_index int not null,
  market    text not null,
  price     numeric not null,
  size      numeric not null,
  side      text not null check (side in ('buy', 'sell')),
  block     bigint not null,
  ts        timestamptz not null,
  primary key (tx_hash, log_index)
);
create index kuru_trades_market_ts on kuru_trades(market, ts);

create table nav_points (
  basket        text not null,
  ts            timestamptz not null,
  nav_per_share numeric not null,
  total_nav     numeric not null,
  supply        numeric not null,
  primary key (basket, ts)
);

create table indexer_cursor (
  name  text primary key,
  block bigint not null
);
