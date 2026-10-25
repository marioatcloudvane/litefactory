-- Why the daemon holds new runs, on the same display-only row, so status,
-- free-form answers and daemon health can say so; empty while nothing holds.
ALTER TABLE daemon_listener_status ADD COLUMN runs_held TEXT NOT NULL DEFAULT '';
