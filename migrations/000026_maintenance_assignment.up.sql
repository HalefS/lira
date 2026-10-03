-- Let a manager decide who is responsible for a maintenance check.
--
-- assignee_id NULL means the check belongs to everybody: any technician can see
-- it and record it. A user id means it is that person's, and nobody else sees it
-- on the page. Managers always see the whole set either way -- this narrows who
-- has the work, not who is allowed to administer it.
--
-- Chosen over a separate "all" boolean because null is already the answer to
-- "nobody in particular", and a second column would allow the impossible states
-- of both null and true.
--
-- ON DELETE SET NULL rather than RESTRICT: a technician leaving the hotel should
-- release their checks back to the pool, not delete the equipment with them.
-- Losing the name is the point -- the schedule and its history stay, and the work
-- goes back to being everybody's rather than vanishing into a dead account.
--
-- Nothing is filtered at write time. Recording a check is still open to any
-- signed-in user, because the people doing the work are the technicians and a
-- device that quietly became unrecordable would just be skipped.
ALTER TABLE maintenance_schedules
    ADD COLUMN IF NOT EXISTS assignee_id bigint
        REFERENCES users(id) ON DELETE SET NULL;

-- The queue is read as "my work, soonest first", filtered by assignee in the
-- database rather than in the client -- otherwise the rows a technician must not
-- see would still cross the wire.
CREATE INDEX IF NOT EXISTS maintenance_schedules_assignee_idx
    ON maintenance_schedules (assignee_id);