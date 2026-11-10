-- Plain-text access grants, one row per revision. A change closes the open row
-- and adds the next revision; rows are never deleted (except with their
-- repository) and only the close fields may be set, once. The old fixed rights
-- tables were never used and are dropped without converting their rows.
CREATE TABLE project_access_grants (
 id INTEGER PRIMARY KEY,
 repo_id INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
 provider TEXT NOT NULL, api_origin TEXT NOT NULL, actor_id TEXT NOT NULL,
 handle TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision > 0),
 grant_text TEXT NOT NULL CHECK(length(trim(grant_text, ' '||char(9,10,13)))>0 AND length(CAST(grant_text AS BLOB))<=4096),
 created_at TEXT NOT NULL,
 closed_at TEXT,
 close_reason TEXT CHECK(close_reason IN ('replaced','revoked')),
 CHECK((closed_at IS NULL)=(close_reason IS NULL)),
 UNIQUE(repo_id,provider,api_origin,actor_id,revision)
);
CREATE UNIQUE INDEX project_access_grants_open ON project_access_grants(repo_id,provider,api_origin,actor_id) WHERE closed_at IS NULL;
CREATE TRIGGER project_access_grants_no_delete BEFORE DELETE ON project_access_grants
 WHEN EXISTS(SELECT 1 FROM repos WHERE id=OLD.repo_id)
 BEGIN SELECT RAISE(ABORT,'access grants are never deleted'); END;
CREATE TRIGGER project_access_grants_close_once BEFORE UPDATE ON project_access_grants
 WHEN OLD.closed_at IS NOT NULL OR NEW.closed_at IS NULL OR NEW.id<>OLD.id OR NEW.repo_id<>OLD.repo_id
  OR NEW.provider<>OLD.provider OR NEW.api_origin<>OLD.api_origin OR NEW.actor_id<>OLD.actor_id
  OR NEW.handle<>OLD.handle OR NEW.revision<>OLD.revision OR NEW.grant_text<>OLD.grant_text OR NEW.created_at<>OLD.created_at
 BEGIN SELECT RAISE(ABORT,'an access grant may only be closed once'); END;
DROP TABLE rights_history;
DROP TABLE reviewer_assignments;
DROP TABLE rights_grants;
