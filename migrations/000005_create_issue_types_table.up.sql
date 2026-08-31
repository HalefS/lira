CREATE TABLE IF NOT EXISTS issue_types (
    id         bigserial PRIMARY KEY,
    created_at timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    name       citext UNIQUE NOT NULL,
    created_by bigint REFERENCES users ON DELETE SET NULL
);

INSERT INTO issue_types (name) VALUES
    ('Door'),
    ('Internet'),
    ('Hardware'),
    ('TV'),
    ('AC'),
    ('Phone'),
    ('Other')
ON CONFLICT (name) DO NOTHING;
