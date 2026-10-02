-- The heartbeat and discovery facts `daemon status` prints beside the webhook
-- listener, on the same single display-only row: nothing in the daemon reads
-- them back. No new table.
--
-- last_heartbeat_started_at is NULL until the first heartbeat of the publishing
-- daemon. paused_since and expected_resume_at are NULL while discovery is
-- running; a NULL expected_resume_at while paused means "when fresh rate-limit
-- data arrives".
ALTER TABLE daemon_listener_status ADD COLUMN last_heartbeat_started_at TEXT;
ALTER TABLE daemon_listener_status ADD COLUMN discovery_paused INTEGER NOT NULL DEFAULT 0 CHECK (discovery_paused IN (0, 1));
ALTER TABLE daemon_listener_status ADD COLUMN pause_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE daemon_listener_status ADD COLUMN paused_since TEXT;
ALTER TABLE daemon_listener_status ADD COLUMN expected_resume_at TEXT;
ALTER TABLE daemon_listener_status ADD COLUMN runs_in_flight INTEGER NOT NULL DEFAULT 0;
