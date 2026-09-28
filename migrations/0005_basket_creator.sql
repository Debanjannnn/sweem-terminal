ALTER TABLE baskets ADD COLUMN IF NOT EXISTS creator text;
CREATE INDEX IF NOT EXISTS baskets_creator_idx ON baskets (creator);
