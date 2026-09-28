-- the executor's backend signer address, empty when the owner's own wallet
-- signs its trades. with no backend signer there is no TRADE grant to make or
-- revoke, and the client must not offer one.
alter table kuru_accounts add column if not exists signer text not null default '';
