-- Makes the LCU trial period a manager-tunable setting instead of a constant.
--
-- app_settings.lcu_window_days is what a *new* reader is trialled for, and is
-- stored alongside the recurring-issue window on the same Admin screen.
--
-- lcu_units.window_days records the length a given unit was actually put on
-- trial for, alongside starts_on and ends_on which already pin its window as a
-- historical fact. Each unit therefore keeps the log it was given: changing the
-- setting from 7 to 14 does not rewrite a reader that is three days into a
-- seven-day trial, grow its verdict, or retroactively demand days that were
-- never asked for. Existing rows default to 7, which is exactly the window they
-- were created under.
ALTER TABLE app_settings
    ADD COLUMN lcu_window_days integer NOT NULL DEFAULT 7
    CHECK (lcu_window_days BETWEEN 1 AND 60);

ALTER TABLE lcu_units
    ADD COLUMN window_days integer NOT NULL DEFAULT 7
    CHECK (window_days BETWEEN 1 AND 60);
