-- a user is a privy DID plus the embedded wallet it signs with
create table if not exists users (
    id              text primary key,
    wallet_address  text unique,
    privy_wallet_id text,
    created_at      timestamptz not null default now(),
    first_funded_at timestamptz
);
