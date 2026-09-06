-- A blocked schedule may need a factory reply after a prerequisite fails.
-- Recording only after the forge post keeps retrying the safe direction.
CREATE TABLE schedule_block_replies (
    schedule_id INTEGER PRIMARY KEY REFERENCES schedules (id) ON DELETE CASCADE,
    replied_at  TEXT NOT NULL
);
