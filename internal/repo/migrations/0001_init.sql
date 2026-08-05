-- Games collection. Statuses: wishlist (wish to play/buy), playing, played.
CREATE TABLE IF NOT EXISTS games (
    id         BIGSERIAL PRIMARY KEY,
    title      TEXT NOT NULL,
    status     TEXT NOT NULL CHECK (status IN ('wishlist', 'playing', 'played')),
    rating     SMALLINT NOT NULL DEFAULT 0 CHECK (rating BETWEEN 0 AND 5),
    platform   TEXT NOT NULL DEFAULT '',
    year       INTEGER,
    genre      TEXT NOT NULL DEFAULT '',
    cover_url  TEXT NOT NULL DEFAULT '',
    notes      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_games_status ON games (status);
