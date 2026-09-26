-- How long each third-party company actually took to come back with a solution.
--
-- This is deliberately NOT the issue's own time_minutes: that measures the
-- work our technicians did within a shift, while this measures a company's
-- turnaround waiting time, which routinely spans hours or days. Because the two
-- measure different things, these columns live on the handover row and are
-- never folded into the team's average resolution time or the analytics page.
--
-- started_at / resolved_at are the wall-clock moments we reported the problem
-- and the moment the company gave us a solution. duration_minutes is derived
-- from them and stored, the same way `issues.time_minutes` is stored, so the
-- statistics never depend on re-reading the timestamps and stay correct if a
-- row is later edited.
--
-- Existing handovers are backfilled from created_at: we always know roughly
-- when we logged the handover, and that is the closest honest answer for rows
-- recorded before these columns existed.
ALTER TABLE issue_support_requests
    ADD COLUMN IF NOT EXISTS started_at      timestamp(0) with time zone,
    ADD COLUMN IF NOT EXISTS resolved_at     timestamp(0) with time zone,
    ADD COLUMN IF NOT EXISTS duration_minutes integer;

UPDATE issue_support_requests SET started_at = created_at WHERE started_at IS NULL;

-- A solution can never be dated before the problem was reported, a duration is
-- never negative, a duration only exists once there is a resolution, and a
-- handover marked solved always has both — otherwise it would silently drop out
-- of the per-company averages. A handover that is still pending may keep the
-- timestamps it had, so reopening a solved one does not erase its history.
ALTER TABLE issue_support_requests ADD CONSTRAINT issue_support_requests_times CHECK (
    (resolved_at IS NULL OR resolved_at >= started_at)
    AND (duration_minutes IS NULL OR duration_minutes >= 0)
    AND (duration_minutes IS NULL OR resolved_at IS NOT NULL)
    AND (status <> 'solved' OR (resolved_at IS NOT NULL AND duration_minutes IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS issue_support_requests_created_at_idx ON issue_support_requests (created_at);
CREATE INDEX IF NOT EXISTS issue_support_requests_duration_idx  ON issue_support_requests (duration_minutes);
