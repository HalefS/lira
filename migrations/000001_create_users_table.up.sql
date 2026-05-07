-- migrations/000001_create_users_table.up.sql
CREATE TABLE IF NOT EXISTS users (
    id         bigserial PRIMARY KEY,
    created_at timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    name       text NOT NULL,
    email      citext UNIQUE NOT NULL,
    password_hash bytea NOT NULL,
    role       text NOT NULL DEFAULT 'technician' CHECK (role IN ('technician', 'manager')),
    avatar_idx integer NOT NULL DEFAULT 0,
    version    integer NOT NULL DEFAULT 1
);
