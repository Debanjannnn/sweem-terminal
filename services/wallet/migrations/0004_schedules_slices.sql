create table if not exists schedules (
    id               uuid primary key default gen_random_uuid(),
    user_id          text        not null references users (id),
    kind             text        not null check (kind in ('twap', 'dca', 'grid')),
    market_id        text        not null,
    venue            text        not null,
    side             text        not null,
    total            numeric(78, 0) not null,
    slice_count      int         not null,
    window_seconds   bigint      not null default 0,
    cadence_seconds  bigint      not null default 0,
    levels           jsonb,
    state            text        not null default 'active' check (state in ('active', 'paused_revoked', 'cancelled', 'completed')),
    idempotency_key  text        not null,
    created_at       timestamptz not null default now(),
    unique (user_id, idempotency_key)
);

create table if not exists slices (
    schedule_id uuid   not null references schedules (id) on delete cascade,
    slice_index int    not null,
    due_at      timestamptz not null,
    amount      numeric(78, 0) not null,
    state       text   not null default 'pending' check (state in ('pending', 'dispatched', 'filled', 'failed', 'skipped')),
    intent_key  text,
    skip_reason text   not null default '',
    primary key (schedule_id, slice_index)
);

-- the scheduler's hot query
create index if not exists slices_due on slices (state, due_at);
