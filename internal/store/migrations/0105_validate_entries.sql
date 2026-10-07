-- One request to start Validate for one Implement result: the parent session
-- and the integration-branch commit it pinned. The row is saved before
-- admission is attempted, and a second start for the same key finds it instead
-- of starting another run. A refused entry admits nothing and is reopened by a
-- later start. An admitted entry always names its Validate graph instance.
CREATE TABLE validate_entries (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 session_id INTEGER NOT NULL CHECK(session_id>0),
 commit_sha TEXT NOT NULL CHECK(length(commit_sha) IN (40,64)),
 branch TEXT NOT NULL CHECK(length(branch)>0),
 source TEXT NOT NULL CHECK(source IN ('owner','auto')),
 comment_id INTEGER NOT NULL DEFAULT 0 CHECK(comment_id>=0),
 author_id INTEGER NOT NULL DEFAULT 0 CHECK(author_id>=0),
 note TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL CHECK(state IN ('requested','admitted','refused')),
 reason TEXT NOT NULL DEFAULT '',
 instance_id INTEGER NOT NULL DEFAULT 0 CHECK(instance_id>=0),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 UNIQUE(session_id,commit_sha),
 CHECK((state='admitted')=(instance_id>0)),
 CHECK((state='refused')=(length(reason)>0))
);
