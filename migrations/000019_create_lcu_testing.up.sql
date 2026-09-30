-- LCU testing: a failed door card reader is put on trial for a week before it
-- is scrapped, and the week of results is kept against the reader.
--
-- The physical test is simple — hold an RFID card against the reader. A green
-- light means it read the card and the reader passes; red means it did not and
-- the reader fails. A technician does that once a day for seven days, because a
-- reader that works on Monday can still be intermittent by Friday.
--
-- `starts_on` is day 1 of the window and `ends_on` is day 7. Both are plain
-- dates rather than timestamps: the window is a run of calendar days in the
-- hotel's local time, not a duration, so storing a date keeps it immune to DST
-- shifts and to the reader's timezone disagreeing with the server's.
--
-- `status` stays 'active' for the whole window even after a day fails. The
-- verdict is only reached at the end, and only in one way: seven recorded days
-- that all passed. A day with no result is not a pass, so a unit the technician
-- forgot about cannot be signed off.
--
-- Re-testing a reader that already failed is a new row rather than a reused
-- one, so the serial is deliberately not unique: the same reader can go
-- through the process more than once, and each attempt keeps its own history.
CREATE TABLE IF NOT EXISTS lcu_units (
    id          bigserial PRIMARY KEY,
    created_at  timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    -- The serial printed on the reader itself.
    serial      text NOT NULL CHECK (length(trim(serial)) > 0),
    added_by    bigint REFERENCES users ON DELETE SET NULL,
    starts_on   date NOT NULL,
    ends_on     date NOT NULL CHECK (ends_on >= starts_on),
    status      text NOT NULL DEFAULT 'active'
                CHECK (status IN ('active', 'passed', 'failed')),
    resolved_at timestamp(0) with time zone,
    note        text NOT NULL DEFAULT ''
);

-- Finding a reader's serial when someone is asked about it, and sweeping the
-- units whose window has closed.
CREATE INDEX IF NOT EXISTS lcu_units_added_by_idx ON lcu_units (added_by, status);
CREATE INDEX IF NOT EXISTS lcu_units_status_idx  ON lcu_units (status, ends_on);
CREATE INDEX IF NOT EXISTS lcu_units_serial_idx   ON lcu_units (lower(trim(serial)));

-- One row per unit per day: the result the technician gave that day, and who
-- gave it. Re-answering the same day overwrites the row rather than appending,
-- so "did it pass on the 4th" always has exactly one answer. created_at and
-- updated_at are both kept so a correction is visible as a correction.
--
-- ON DELETE CASCADE is deliberate: a unit's daily log has no meaning without the
-- unit, and unlike a stock movement there is no stock to give back.
CREATE TABLE IF NOT EXISTS lcu_tests (
    id         bigserial PRIMARY KEY,
    unit_id    bigint NOT NULL REFERENCES lcu_units ON DELETE CASCADE,
    day        date NOT NULL,
    result     text NOT NULL CHECK (result IN ('pass', 'fail')),
    note       text NOT NULL DEFAULT '',
    logged_by  bigint REFERENCES users ON DELETE SET NULL,
    created_at timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    updated_at timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    UNIQUE (unit_id, day)
);

CREATE INDEX IF NOT EXISTS lcu_tests_unit_day_idx ON lcu_tests (unit_id, day);
CREATE INDEX IF NOT EXISTS lcu_tests_logged_by_idx ON lcu_tests (logged_by);
