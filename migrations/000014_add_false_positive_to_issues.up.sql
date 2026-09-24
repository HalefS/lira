-- Flags issues that turned out to be a false alarm: something was reported,
-- but when a technician checked, nothing was actually wrong. Kept as a separate
-- flag (rather than a third status) so the issue still has a normal Ok/Pending
-- status, and so analytics can count false alarms on their own.
ALTER TABLE issues ADD COLUMN IF NOT EXISTS false_positive boolean NOT NULL DEFAULT false;
