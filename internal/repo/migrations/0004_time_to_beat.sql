-- HowLongToBeat enrichment: average main-story completion time,
-- fetched from HowLongToBeat (Steam's API has no such data).
ALTER TABLE games ADD COLUMN IF NOT EXISTS time_to_beat_minutes INTEGER;
