UPDATE issue_types SET tracks_swaps = false WHERE lower(btrim(name)) = 'tv';

ALTER TABLE issue_types
    DROP COLUMN IF EXISTS tracks_swaps;

DROP INDEX IF EXISTS issue_tv_swaps_created_idx;
DROP INDEX IF EXISTS issue_tv_swaps_issue_idx;
DROP TABLE IF EXISTS issue_tv_swaps;
