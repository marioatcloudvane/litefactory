-- A source comment is not a run. Commit its original proposal association in
-- the proposal-retention transaction so recovery cannot refresh current state.
CREATE TABLE creation_preparations (
 session_id INTEGER NOT NULL REFERENCES sessions(id),
 comment_id INTEGER NOT NULL CHECK(comment_id > 0),
 owner_id INTEGER NOT NULL CHECK(owner_id > 0),
 proposal_hash TEXT NOT NULL REFERENCES mcp_creation_proposals(hash),
 PRIMARY KEY(session_id,comment_id)
);
