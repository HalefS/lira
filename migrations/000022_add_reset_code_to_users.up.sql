-- The code a manager hands to a member of staff when they reset a password.
--
-- Only the hash is kept. The plaintext exists once, in the response to the
-- manager who generated it, so it can be read out to the user; it cannot be
-- recovered from the database afterwards. bcrypted rather than hashed with
-- SHA-256 because six digits is only a million possibilities, which a fast
-- hash would let anyone with a database dump simply enumerate.
--
-- The code lives on the user rather than in its own table because there is
-- never more than one live code per account: generating a new one overwrites
-- this pair, and clearing the reset nulls it.
ALTER TABLE users ADD COLUMN reset_code_hash bytea;

ALTER TABLE users ADD COLUMN reset_code_expires_at timestamp(0) with time zone;
