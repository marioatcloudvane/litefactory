-- Which sessions hold an in-flight slot and how busy the shared worker pool is,
-- on the same display-only row, so the status lookup can tell task work from
-- housekeeping and say why queued work waits. in_flight_sessions is a
-- comma-separated list of session ids; empty for a daemon that predates it.
ALTER TABLE daemon_listener_status ADD COLUMN in_flight_sessions TEXT NOT NULL DEFAULT '';
ALTER TABLE daemon_listener_status ADD COLUMN slots_used INTEGER NOT NULL DEFAULT 0;
ALTER TABLE daemon_listener_status ADD COLUMN slots_total INTEGER NOT NULL DEFAULT 0;
