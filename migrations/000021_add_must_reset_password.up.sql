-- Forces a user to choose a new password at their next sign-in.
--
-- Set by a manager from the team panel when a password needs changing (staff
-- turnover, a suspected compromise). No password is created or guessed: the
-- account keeps its existing hash until the user proves they know it and picks
-- a replacement.
--
-- Login refuses to issue a session while this is true, so a reset actually
-- takes effect: the old password cannot be used until the user acts on it.
ALTER TABLE users ADD COLUMN must_reset_password boolean NOT NULL DEFAULT false;
