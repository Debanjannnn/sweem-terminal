-- a kuru order names the account by uint40 id, and that id only exists after an
-- AccountCore deposit. cached here so a read does not cost an executor round
-- trip; refreshed from the executor's read-only /account/ensure.
create table if not exists kuru_accounts (
    user_id           text primary key references users (id),
    chain_id          bigint         not null,
    kuru_user_id      bigint         not null default 0,
    signer_authorized boolean        not null default false,
    -- every grant or revoke advances this and invalidates pre-signed intents
    auth_nonce        numeric(78, 0) not null default 0,
    account_core      text           not null default '',
    -- unix seconds. the grant expiry emits no event and cannot be read back, so
    -- the expiry this service asked for is the only record of it.
    signer_expiry     bigint         not null default 0,
    updated_at        timestamptz    not null default now()
);

-- a maker order is priced in raw ticks and sized in book units; neither is the
-- taker leg's raw atoms. size stays the cap denominator for every order type.
alter table intents add column if not exists quantity          numeric(78, 0);
alter table intents add column if not exists tif               text    not null default '';
alter table intents add column if not exists post_only         boolean not null default false;
-- "base" or "quote" on a deposit, empty everywhere else
alter table intents add column if not exists token             text    not null default '';
alter table intents add column if not exists slot_idx          int;
alter table intents add column if not exists expected_order_id numeric(78, 0);
