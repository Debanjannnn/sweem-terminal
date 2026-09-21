-- a grid level re-arms and fires again on the same (schedule_id, slice_index).
-- intent_key is derived from those two alone, so the second fire produced the
-- same key and wallet replayed the first execution instead of placing an order:
-- the double-spend guard silently beat the grid. generation disambiguates the
-- fires; twap/dca never re-arm and stay at 0.
alter table slices add column if not exists generation int not null default 0;
