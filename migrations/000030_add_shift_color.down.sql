-- Drops the shift tint.
--
-- One nullable column on a table that already exists, and nothing anywhere
-- references it, so there is nothing else to unwind. The constraint goes first
-- because it is defined in terms of the column.
--
-- This is the one migration in the rota whose loss is total and unrecoverable in a
-- way a reader might not expect: every tint a manager ever chose goes with it,
-- because a tint is configuration rather than a record of anything. That is the
-- same bargain 000029's down migration states for the rota itself, and it is why
-- the column is nullable -- a shift that predates the feature needs no backfill.
ALTER TABLE shifts DROP CONSTRAINT IF EXISTS shifts_color_check;
ALTER TABLE shifts DROP COLUMN IF EXISTS color;