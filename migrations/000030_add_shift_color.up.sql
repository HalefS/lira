-- A tint colour for a shift, so the grid can tell Morning from Late from Night
-- without the reader decoding the hours.
--
-- NULL is the normal state and means "no opinion": such a shift renders exactly as
-- it does today. In particular a shift that crosses midnight KEEPS its 3px orange
-- left edge, because orange already encodes "overnight" (the .is-night rules in
-- index.html) and a tint that overwrote it would take that meaning away from
-- every night shift the hotel already has. The client rule is
--
--     tint  ||  (overnight ? orange : none)
--
-- and not `tint`. A colour is a manager's display hint; it never gets to make
-- claims about the hours.
--
-- Optional, and nullable rather than NOT NULL DEFAULT ''. issue_types.color
-- (migration 000009) made the other choice and has paid for it ever since: that
-- column holds CSS class names, not colours -- 'badge-door', 'badge-sky' -- so
-- every reader of it has to know that '', a hex and a class name are three
-- different states, and internal/report/daily.go carries a fallback palette
-- because the data allows it. One absent value, one meaning.
--
-- Stored with the leading '#', lower case. The '#' is mandatory out because the
-- value is interpolated straight into a style attribute and a reader should not
-- have to know whether it is there; the lower case is because that is exactly
-- what <input type="color"> hands back, so the round trip is a store of the
-- string the browser already produced rather than a reformatting of it. Same
-- reasoning as issues.start_time being text (migration 000007).
ALTER TABLE shifts
    ADD COLUMN IF NOT EXISTS color text;

-- The database half of data.NormaliseShiftColor, and deliberately the same
-- expression as data.hexColorPattern (internal/data/issue_types.go) so the Go
-- regexp and this CHECK cannot drift apart. Both accept either case, so a
-- hand-written upper-case row still renders; Go folds to lower on the way in.
--
-- Postgres has no IF NOT EXISTS for ADD CONSTRAINT, which is why 000027 writes
-- its constraint bare too. Re-running this migration after a manual rollback of
-- the column needs the DROP in the down migration first.
ALTER TABLE shifts
    ADD CONSTRAINT shifts_color_check
    CHECK (color IS NULL OR color ~ '^#[0-9a-fA-F]{6}$');

COMMENT ON COLUMN shifts.color IS
    'Optional #rrggbb tint for the shift''s cell, picker row and legend entry. NULL means no tint: the shift renders as before, keeping its orange overnight edge.';