-- market-data read plane. numeric(78,0) everywhere: prices and sizes are WAD
-- integers and a float would round them.

create table if not exists ticks (
    market_id text          not null,
    ts        bigint        not null,   -- unix milliseconds
    venue     text          not null,
    price     numeric(78,0) not null,
    size      numeric(78,0) not null default 0,
    side      text          not null default '',
    primary key (market_id, ts, venue)
);

create index if not exists ticks_market_ts_idx on ticks (market_id, ts desc);
create index if not exists ticks_ts_idx on ticks (ts);

create table if not exists candles (
    market_id  text          not null,
    resolution int           not null,  -- seconds
    open_time  bigint        not null,  -- unix seconds, floored to resolution
    o          numeric(78,0) not null,
    h          numeric(78,0) not null,
    l          numeric(78,0) not null,
    c          numeric(78,0) not null,
    v          numeric(78,0) not null default 0,
    primary key (market_id, resolution, open_time)
);

create index if not exists candles_lookup_idx on candles (market_id, resolution, open_time desc);
