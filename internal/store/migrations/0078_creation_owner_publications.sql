-- The factory publishes an approved creation proposal itself, on the owner's
-- approval, instead of waiting for an agent run to bind a grant. One row per
-- approval intent is the idempotency key: a retried approval comment resumes or
-- reports this row and never dispatches a second batch.
CREATE TABLE creation_owner_publications (
 intent_id INTEGER PRIMARY KEY REFERENCES mcp_creation_intents(id),
 repository TEXT NOT NULL,
 proposal_hash TEXT NOT NULL REFERENCES mcp_creation_proposals(hash),
 artifacts INTEGER NOT NULL CHECK(artifacts BETWEEN 1 AND 5),
 state TEXT NOT NULL CHECK(state IN ('dispatched','confirmed','uncertain','refused')),
 reason TEXT NOT NULL DEFAULT '',
 confirmed_ns INTEGER NOT NULL DEFAULT 0,
 created_ns INTEGER NOT NULL
);
CREATE TABLE creation_owner_artifacts (
 intent_id INTEGER NOT NULL REFERENCES creation_owner_publications(intent_id),
 ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 0 AND 4),
 issue_id INTEGER NOT NULL DEFAULT 0,
 issue_number INTEGER NOT NULL DEFAULT 0,
 relationship TEXT NOT NULL DEFAULT 'pending' CHECK(relationship IN ('pending','uncertain','linked','attached')),
 PRIMARY KEY(intent_id,ordinal)
);
