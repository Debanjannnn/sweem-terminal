-- perpl perps: leverage in hundredths on open orders (500 = 5x), 0 elsewhere.
alter table intents add column if not exists leverage int not null default 0;
-- the venue's handle for a resting order, set once the executor reports it:
-- a kuru order id (with slot_idx) or a perpl order id. what a cancel names.
alter table intents add column if not exists order_id text not null default '';
