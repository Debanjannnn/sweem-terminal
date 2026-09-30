-- indexer (mainnet 143): per-user chain events. Writer: indexer only.

-- Kuru V1 Trade where our wallet is maker or taker. One row per (log, role); side is the wallet's side.
create table if not exists user_trades (
  tx_hash      text not null,
  log_index    int not null,
  role         text not null check (role in ('maker', 'taker')),
  market       text not null,
  wallet       text not null,
  side         text not null check (side in ('buy', 'sell')),
  order_id     bigint not null,  -- maker order id
  price        numeric not null, -- raw, 1e18
  size         numeric not null, -- filledSize, market sizePrecision
  updated_size numeric not null, -- maker order remaining after fill
  block        bigint not null,
  ts           timestamptz not null,
  primary key (tx_hash, log_index, role)
);
create index if not exists user_trades_wallet on user_trades(wallet, block desc);

-- Kuru V1 OrderCreated / OrdersCanceled for our wallets (one row per order id).
create table if not exists user_orders_events (
  tx_hash   text not null,
  log_index int not null,
  order_id  bigint not null,
  market    text not null,
  wallet    text not null,
  kind      text not null check (kind in ('created', 'canceled')),
  side      text check (side in ('buy', 'sell')), -- created only
  price     numeric,                              -- created only, market pricePrecision
  size      numeric,                              -- created only, market sizePrecision
  block     bigint not null,
  ts        timestamptz not null,
  primary key (tx_hash, log_index, order_id)
);
create index if not exists user_orders_events_wallet on user_orders_events(wallet, market, order_id);

-- Venue position events for our wallets. amount/shares raw base units.
create table if not exists venue_events (
  tx_hash   text not null,
  log_index int not null,
  venue     text not null, -- aave|neverland|euler|morpho|shmonad|activevault|perpl|treasury
  contract  text not null, -- emitter
  kind      text not null, -- supply|withdraw|deposit|fee|collateral_deposit|collateral_withdraw|position_open|position_increase|position_decrease|position_close
  wallet    text not null,
  asset     text,          -- token address; perpl positions: 'perp:<id>'
  amount    numeric,
  shares    numeric,
  extra     jsonb,
  block     bigint not null,
  ts        timestamptz not null,
  primary key (tx_hash, log_index)
);
create index if not exists venue_events_wallet on venue_events(wallet, block desc);

-- Perpl account id -> wallet (from AccountCreated).
create table if not exists perpl_accounts (
  account_id numeric primary key,
  wallet     text not null
);

-- Wallets already covered by cursor `name`; a new delegation wallet rewinds that cursor.
create table if not exists indexed_wallets (
  name   text not null,
  wallet text not null,
  primary key (name, wallet)
);
