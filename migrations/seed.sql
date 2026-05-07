-- migrations/seed.sql
-- Run after migrations: psql $HOTELITDB_DSN -f ./migrations/seed.sql
-- Passwords: manager123 / eder123 / alcidio123

INSERT INTO users (name, email, password_hash, role, avatar_idx) VALUES
  ('Manager',  'manager@hotel.com',  '$2a$12$lQCBz./e6t9S1oqQ.4K3V.0TFvKBqhYVTEiDT9fHD6Qcz9xMqtGLS', 'manager',    0),
  ('Eder',     'eder@hotel.com',     '$2a$12$lCc5iOFh4B1UF1Wd99Gm1ejXpECjC5ql/.VWZ/YkPMuCbcSMpK.2i', 'technician', 1),
  ('Alcidio',  'alcidio@hotel.com',  '$2a$12$pBqOhgX5R2tHKYlW1FJDiuLQ6ymfTCbgxXXf.U3qkENLEgH9HriPe', 'technician', 2)
ON CONFLICT (email) DO NOTHING;

-- NOTE: The password hashes above are placeholders.
-- To generate real hashes, register via the API or use the /v1/users endpoint.
-- Example:
--   curl -X POST localhost:4000/v1/users \
--     -H "Content-Type: application/json" \
--     -d '{"name":"Manager","email":"manager@hotel.com","password":"manager123","role":"manager"}'
