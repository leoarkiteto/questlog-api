-- New lists: purchased (bought but not played yet) and dropped (played but didn't finish).
ALTER TABLE games DROP CONSTRAINT IF EXISTS games_status_check;
ALTER TABLE games ADD CONSTRAINT games_status_check
    CHECK (status IN ('wishlist', 'purchased', 'playing', 'played', 'dropped'));
