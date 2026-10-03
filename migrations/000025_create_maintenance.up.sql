-- Preventive maintenance: scheduled checks on department equipment.
--
-- Scoped deliberately to what this hotel actually maintains -- printers and
-- phones, all of them department equipment, none of them per-room. Rooms are
-- repaired when they break (that is what the issues table is for); departments
-- get looked after on a calendar whether or not anything is wrong.
--
-- Two tables rather than one. A maintenance_schedules row is a device to keep an
-- eye on: its name, what it is, which department it belongs to, and how often it
-- wants looking at. A maintenance_checks row is one occasion someone actually did
-- that. Keeping them apart is the whole point of the feature: most checks find
-- nothing, and a check that found nothing must not become an issue, or the
-- issue count, the average resolution time and the recurring-alert detection all
-- get diluted by a steady stream of "checked the printer, it was fine".
--
-- A check that DID find something can point at a real issue through issue_id.
-- That is a pointer, not a copy: the issue keeps its own record of what was
-- wrong and what was done, and deleting it leaves the check standing as the
-- historical fact that a fault was found on that date. The link is therefore
-- nullable and cleared rather than cascading.
--
-- Per-device rather than per-department, because a department with a printer at
-- the front desk and another in the back office has two things that can break at
-- two different times. device_name is the label the technician recognises; it is
-- free text because only the hotel knows what it calls its own equipment.
--
-- department_id is a real reference rather than the department's name copied in,
-- so renaming a department does not leave maintenance history pointing at a name
-- that no longer exists.
--
-- next_due_on is stored rather than derived per read. It is a cache of
-- last_done_on + interval_days (or created_on + interval_days while a device has
-- never been done), and the single rule that fills it lives in the application
-- rather than being written out again in every query. Keeping it on the row is
-- what lets the queue be ordered by it in the database.
--
-- Because due dates run from the last time the work was actually done rather
-- than from a calendar, a device cannot be brought up to date by marking it done
-- early -- which is the point. next_due_on only moves when a check is recorded.
CREATE TABLE IF NOT EXISTS maintenance_schedules (
    id           bigserial PRIMARY KEY,
    created_at   timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    updated_at   timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    version      integer NOT NULL DEFAULT 1,

    -- What the technician calls it: "Reception front desk", "Kitchen pass".
    device_name  text NOT NULL,
    equipment_type text NOT NULL,
    -- A real reference rather than the department's name copied in, so renaming
    -- a department does not leave maintenance history pointing at a name that no
    -- longer exists. RESTRICT rather than CASCADE: silently deleting every
    -- maintenance record for a department because someone renamed or removed it
    -- would destroy a service history that cannot be recovered from anywhere else.
    department_id bigint NOT NULL REFERENCES departments(id) ON DELETE RESTRICT,
    -- How often it wants looking at, in days. Seven days is a weekly round; two
    -- years is a yearly service. Anything tighter than a week is a bug report,
    -- not maintenance.
    interval_days integer NOT NULL,
    active       boolean NOT NULL DEFAULT true,

    -- The last recorded check, copied onto the schedule so the queue can be read
    -- without joining the whole history of every device. The checks table remains
    -- the record of truth.
    last_done_on date,
    last_outcome text,
    last_note    text NOT NULL DEFAULT '',

    next_due_on  date NOT NULL,

    created_by   bigint REFERENCES users(id) ON DELETE SET NULL,
    updated_by   bigint REFERENCES users(id) ON DELETE SET NULL,

    CONSTRAINT maintenance_schedules_equipment_type_check
        CHECK (equipment_type IN ('printer', 'phone')),
    CONSTRAINT maintenance_schedules_interval_days_check
        CHECK (interval_days BETWEEN 7 AND 730),
    CONSTRAINT maintenance_schedules_last_outcome_check
        CHECK (last_outcome IS NULL OR last_outcome IN ('ok', 'fault'))
);

-- The queue is read as "what is due, soonest first", over active devices only.
CREATE INDEX IF NOT EXISTS maintenance_schedules_due_idx
    ON maintenance_schedules (active, next_due_on);

CREATE TABLE IF NOT EXISTS maintenance_checks (
    id          bigserial PRIMARY KEY,
    created_at  timestamp(0) with time zone NOT NULL DEFAULT NOW(),

    schedule_id bigint NOT NULL
                REFERENCES maintenance_schedules(id) ON DELETE CASCADE,
    -- Denormalised rather than read back from created_at so a check can be filed
    -- against the day the work was actually done, which is not always the day it
    -- was recorded.
    done_on     date NOT NULL,

    outcome     text NOT NULL,
    note        text NOT NULL DEFAULT '',

    done_by     bigint NOT NULL REFERENCES users(id) ON DELETE RESTRICT,

    -- A fault found during this check, as a normal issue. Set when the technician
    -- raises one, and cleared if that issue is later deleted rather than taking
    -- the historical fact of the check with it.
    --
    -- Deliberately optional even when outcome is 'fault'. A fault found and put
    -- right on the spot is a real thing that really happened and belongs in the
    -- history, and demanding an issue row for it would push people into logging
    -- throwaway issues -- exactly the noise this feature exists to avoid.
    issue_id    bigint REFERENCES issues(id) ON DELETE SET NULL,

    CONSTRAINT maintenance_checks_outcome_check
        CHECK (outcome IN ('ok', 'fault'))
);

-- The history of one device, newest first. Covers the only query the UI makes
-- against this table.
CREATE INDEX IF NOT EXISTS maintenance_checks_schedule_idx
    ON maintenance_checks (schedule_id, done_on DESC, id DESC);