ALTER TABLE issues
    DROP COLUMN IF EXISTS reported_by_agent,
    DROP COLUMN IF EXISTS confirmed_by_agent;

DROP TABLE IF EXISTS connecta_agents;
