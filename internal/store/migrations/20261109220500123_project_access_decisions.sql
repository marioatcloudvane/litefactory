-- One row per grantee access check, keyed by the source comment that asked. A
-- check starts pending and becomes final exactly once; a final row is never
-- changed, and rows are never deleted except with their repository.
CREATE TABLE project_access_decisions (
 id INTEGER PRIMARY KEY,
 repo_id INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
 session_id INTEGER NOT NULL,
 source_provider TEXT NOT NULL, source_origin TEXT NOT NULL, source_id TEXT NOT NULL,
 covered_through TEXT,
 actor_provider TEXT NOT NULL, actor_origin TEXT NOT NULL, actor_id TEXT NOT NULL,
 handle TEXT NOT NULL,
 grant_id INTEGER, grant_revision INTEGER,
 target_json TEXT NOT NULL,
 request_digest TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('pending','final')),
 decision TEXT CHECK(decision IN ('allow','deny','floor','error','limited','rate_limited')),
 reason TEXT,
 model TEXT NOT NULL DEFAULT '',
 classified INTEGER NOT NULL DEFAULT 0 CHECK(classified IN (0,1)),
 created_at TEXT NOT NULL,
 decided_at TEXT,
 CHECK((state='pending' AND decision IS NULL AND reason IS NULL AND decided_at IS NULL)
  OR (state='final' AND decision IS NOT NULL AND length(trim(COALESCE(reason,''), ' '||char(9,10,13)))>0 AND decided_at IS NOT NULL)),
 UNIQUE(repo_id,source_provider,source_origin,source_id)
);
CREATE INDEX project_access_decisions_actor ON project_access_decisions(repo_id,actor_provider,actor_origin,actor_id,created_at);
CREATE TRIGGER project_access_decisions_no_delete BEFORE DELETE ON project_access_decisions
 WHEN EXISTS(SELECT 1 FROM repos WHERE id=OLD.repo_id)
 BEGIN SELECT RAISE(ABORT,'access decisions are never deleted'); END;
CREATE TRIGGER project_access_decisions_finish_once BEFORE UPDATE ON project_access_decisions
 WHEN OLD.state<>'pending' OR NEW.state<>'final' OR NEW.id<>OLD.id OR NEW.repo_id<>OLD.repo_id OR NEW.session_id<>OLD.session_id
  OR NEW.source_provider<>OLD.source_provider OR NEW.source_origin<>OLD.source_origin OR NEW.source_id<>OLD.source_id
  OR NEW.covered_through IS NOT OLD.covered_through
  OR NEW.actor_provider<>OLD.actor_provider OR NEW.actor_origin<>OLD.actor_origin OR NEW.actor_id<>OLD.actor_id
  OR NEW.handle<>OLD.handle OR NEW.grant_id IS NOT OLD.grant_id OR NEW.grant_revision IS NOT OLD.grant_revision
  OR NEW.target_json<>OLD.target_json OR NEW.request_digest<>OLD.request_digest OR NEW.created_at<>OLD.created_at
 BEGIN SELECT RAISE(ABORT,'an access decision may only be finished once'); END;
