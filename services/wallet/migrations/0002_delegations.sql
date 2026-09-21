create table if not exists delegations (
    user_id                text primary key references users (id) on delete cascade,
    signer_id              text        not null,
    policy_id              text,
    venues                 text[]      not null,
    markets                text[]      not null,
    max_notional_per_slice numeric(78, 0) not null,
    max_notional_total     numeric(78, 0) not null,
    expires_at             timestamptz not null,
    revoked_at             timestamptz,
    constraint delegation_caps_ordered check (max_notional_per_slice <= max_notional_total)
);

create table if not exists delegation_audit (
    id      bigserial primary key,
    user_id text        not null,
    action  text        not null,
    detail  jsonb       not null default '{}'::jsonb,
    at      timestamptz not null default now()
);

create index if not exists delegation_audit_user_at on delegation_audit (user_id, at desc);

-- append-only in the database, not in application code: an audit row a service
-- can rewrite is not an audit row.
create or replace function delegation_audit_append_only() returns trigger language plpgsql as $$
begin
    raise exception 'delegation_audit is append-only, so % is refused', tg_op;
end;
$$;

drop trigger if exists delegation_audit_immutable on delegation_audit;
create trigger delegation_audit_immutable
    before update or delete on delegation_audit
    for each row execute function delegation_audit_append_only();
