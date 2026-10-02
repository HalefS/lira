-- TV swap alerts: a swap means a working set was carried into a room to cover
-- for a fault, so the room's own equipment is still broken and still needs
-- dealing with. That belongs on the Alerts page, where the rest of the "needs
-- attention" work already lives, rather than only on the TV swaps page where
-- nobody thinks to look.
--
-- One row per swap rather than per issue. An issue can hold up to ten swaps and
-- each of those is a distinct set carried for a distinct room, so they are
-- distinct alerts. The room named is the issue's own location, because a bare
-- "1324 -> 1301" does not say which of the two was the problem -- the same
-- reason the TV swaps page joins the issue's fields in.
--
-- Everything except the verdict is joined in at read time rather than copied
-- here. That is the whole reason an edit to the issue, or to the swap itself,
-- shows up in the alert instead of leaving a stale snapshot of what it used to
-- say. The row is keyed on swap_id and cascades with it, so deleting the issue
-- -- which cascades to its swaps -- takes its alerts with it and leaves nothing
-- behind describing a fault that no longer exists.
--
-- Only the verdict is stored: pending until a manager records that the faulty
-- equipment was dealt with, plus the note they left saying how.
-- solved_swap_version records which version of the swap that verdict was given
-- against, so editing the swap afterwards re-opens the alert rather than leaving
-- a "solved" stamp attached to something that no longer says what it said. The
-- swap table carries no updated_at of its own, so version is the edit signal.
CREATE TABLE IF NOT EXISTS tv_swap_alerts (
    id          bigserial PRIMARY KEY,
    created_at  timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    updated_at  timestamp(0) with time zone NOT NULL DEFAULT NOW(),

    swap_id     bigint NOT NULL UNIQUE
                REFERENCES issue_tv_swaps(id) ON DELETE CASCADE,

    status      text NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending', 'solved')),
    solution    text NOT NULL DEFAULT '',
    solved_by   bigint REFERENCES users ON DELETE SET NULL,
    solved_at   timestamp(0) with time zone,

    -- The swap version this verdict was recorded against. 0 while pending,
    -- which makes any later version an increase.
    solved_swap_version integer NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS tv_swap_alerts_status_idx
    ON tv_swap_alerts (status, updated_at DESC);