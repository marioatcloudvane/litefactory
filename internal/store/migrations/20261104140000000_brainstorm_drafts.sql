ALTER TABLE brainstorm_scans ADD COLUMN purpose TEXT CHECK(purpose IS NULL OR purpose IN ('question','drafts'));
CREATE TABLE brainstorm_draft_sets (
 id TEXT PRIMARY KEY, brainstorm_id TEXT NOT NULL REFERENCES brainstorms(id), scan_id TEXT NOT NULL UNIQUE REFERENCES brainstorm_scans(id),
 seq INTEGER NOT NULL, state TEXT NOT NULL CHECK(state IN ('latest','superseded','used')),
 summary TEXT NOT NULL, created_at TEXT NOT NULL,
 UNIQUE(brainstorm_id,seq)
);
CREATE TABLE brainstorm_drafts (
 id TEXT PRIMARY KEY, draft_set_id TEXT NOT NULL REFERENCES brainstorm_draft_sets(id), key TEXT NOT NULL, ordinal INTEGER NOT NULL,
 title TEXT NOT NULL CHECK(length(CAST(title AS BLOB))<=256), size TEXT NOT NULL CHECK(size IN ('S','M','L')), impact TEXT NOT NULL,
 related_issues TEXT NOT NULL DEFAULT '[]', duplicate_warning TEXT NOT NULL DEFAULT '', search_hits TEXT NOT NULL DEFAULT '',
 ledger TEXT NOT NULL, digest TEXT NOT NULL,
 UNIQUE(draft_set_id,key), UNIQUE(draft_set_id,ordinal)
);
