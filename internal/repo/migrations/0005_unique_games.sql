-- One card per game: a game may appear only once in the collection.
-- Uniqueness key: normalized title OR Steam app id (when linked).
-- This is the database-level backstop for the API-level check in repo.Create/Update.

-- Comparison key: lowercase, trimmed, internal whitespace collapsed to
-- single spaces. Must stay in sync with repo.normalizeTitle (Go), which
-- computes the same value for every new insert/update.
-- Assumes a UTF-8 locale (e.g. en_US.UTF-8): Go's ToLower/Fields and
-- Postgres lower()/[[:space:]] agree on the ASCII game titles this app
-- stores; a non-UTF8 collation could backfill differently than Go writes.
ALTER TABLE games ADD COLUMN IF NOT EXISTS normalized_title TEXT;

-- Backfill existing rows.
UPDATE games
SET normalized_title = lower(btrim(regexp_replace(title, '[[:space:]]+', ' ', 'g')))
WHERE normalized_title IS NULL;

-- Clean up duplicates that already exist before adding the constraints.
-- Keep the most recently added card (highest id) per group, and run the
-- Steam pass FIRST: a row that loses the appid race to a different title
-- must not take its whole title group with it. Example: A("X", NULL),
-- B("X", 100), C("Y", 100) — title-first keeps B, then the appid pass
-- deletes B and "X" vanishes; appid-first deletes B and the title pass
-- falls back to A, preserving the "X" card.
DELETE FROM games a
USING games b
WHERE a.id < b.id
  AND a.steam_appid IS NOT NULL
  AND a.steam_appid = b.steam_appid;

DELETE FROM games a
USING games b
WHERE a.id < b.id
  AND a.normalized_title = b.normalized_title;

ALTER TABLE games ALTER COLUMN normalized_title SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_games_normalized_title ON games (normalized_title);
CREATE UNIQUE INDEX IF NOT EXISTS uq_games_steam_appid ON games (steam_appid) WHERE steam_appid IS NOT NULL;
