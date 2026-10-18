CREATE TABLE doctor_instruction_captures (
    attempt_id TEXT PRIMARY KEY REFERENCES doctor_attempts(id) ON DELETE CASCADE,
    capture BLOB NOT NULL,
    created_at TEXT NOT NULL
);
