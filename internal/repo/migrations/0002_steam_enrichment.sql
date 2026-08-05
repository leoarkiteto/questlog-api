-- Steam enrichment: link a game to its Steam app and keep the
-- description fetched from the store page.
ALTER TABLE games ADD COLUMN IF NOT EXISTS steam_appid BIGINT;
ALTER TABLE games ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';
