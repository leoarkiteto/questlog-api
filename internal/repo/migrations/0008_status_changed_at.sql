-- The library "Recent" sort shows the most recently *entered* status
-- first. status_changed_at records when a game last moved into its
-- current status: set at insert, bumped only by real status changes in
-- repo.Update (notes/rating/metadata edits keep it, so they don't
-- reorder the library).
ALTER TABLE games ADD COLUMN status_changed_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- Existing rows keep their current relative order: backfill from
-- created_at (the add time) so deploying doesn't reshuffle the library.
UPDATE games SET status_changed_at = created_at;

CREATE INDEX IF NOT EXISTS idx_games_status_changed_at ON games (status_changed_at);
