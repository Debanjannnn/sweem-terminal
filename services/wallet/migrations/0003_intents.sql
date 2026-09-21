-- intent_key is the double-spend contract for the whole system: one row per
-- intent, and the executor replays the recorded result for a key it has seen.
create table if not exists intents (
    intent_key       text primary key,
    user_id          text        not null references users (id),
    market_id        text        not null,
    venue            text        not null,
    side             text        not null,
    order_type       text        not null,
    size             numeric(78, 0) not null,
    limit_price      numeric(78, 0),
    max_slippage_bps int         not null default 0,
    chain_id         bigint      not null,
    state            text        not null default 'pending',
    created_at       timestamptz not null default now()
);

create index if not exists intents_user_created on intents (user_id, created_at desc);

create table if not exists executions (
    intent_key   text primary key references intents (intent_key) on delete cascade,
    tx_hash      text        not null default '',
    confirmation text        not null default 'pending',
    error        text        not null default '',
    submitted_at timestamptz not null default now(),
    settled_at   timestamptz
);
