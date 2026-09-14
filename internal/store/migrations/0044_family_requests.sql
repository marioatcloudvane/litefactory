CREATE TABLE family_requests (
    run_id INTEGER NOT NULL REFERENCES runs(id),
    attempt TEXT NOT NULL,
    request_id TEXT NOT NULL,
    repository TEXT NOT NULL,
    task TEXT NOT NULL,
    tool TEXT NOT NULL,
    payload_hash TEXT NOT NULL,
    disposition TEXT NOT NULL CHECK(disposition IN ('pending','rejected','uncertain','confirmed')),
    reference_kind TEXT NOT NULL DEFAULT '',
    reference_id INTEGER NOT NULL DEFAULT 0,
    reference_request TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(run_id, attempt, request_id)
);
CREATE INDEX family_request_lookup ON family_requests(request_id);
