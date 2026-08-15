-- Scope the collection to a user account.
--
-- games.user_id is born NULL so existing rows survive the migration.
-- The first user created via cmd/user adopts the orphan rows
-- (UPDATE games SET user_id = $1 WHERE user_id IS NULL); after that no
-- row is ever ownerless.
--
-- Uniqueness becomes per-user: a game may appear only once in each
-- user's collection, keyed by normalized title OR Steam app id. The
-- global unique indexes from 0005 are replaced by (user_id, ...) ones.
-- NULL user_ids (orphan rows pre-adoption) are distinct in Postgres
-- unique indexes, so they neither block adoption nor conflict.

ALTER TABLE games ADD COLUMN IF NOT EXISTS user_id BIGINT REFERENCES users(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_games_user_id ON games (user_id);

DROP INDEX IF EXISTS uq_games_normalized_title;
DROP INDEX IF EXISTS uq_games_steam_appid;

CREATE UNIQUE INDEX IF NOT EXISTS uq_games_user_normalized_title ON games (user_id, normalized_title);
CREATE UNIQUE INDEX IF NOT EXISTS uq_games_user_steam_appid ON games (user_id, steam_appid) WHERE steam_appid IS NOT NULL;
