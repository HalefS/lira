CREATE TABLE IF NOT EXISTS departments (
    id         bigserial PRIMARY KEY,
    created_at timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    name       citext UNIQUE NOT NULL,
    created_by bigint REFERENCES users ON DELETE SET NULL
);

INSERT INTO departments (name) VALUES
    ('Guest'),
    ('Reception'),
    ('Restaurant'),
    ('Gym'),
    ('Spa'),
    ('Kitchen'),
    ('Conference'),
    ('Back Office')
ON CONFLICT (name) DO NOTHING;
