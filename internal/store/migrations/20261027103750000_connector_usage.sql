CREATE TABLE connector_usage_targets (
 grant_id TEXT NOT NULL REFERENCES connector_grants(id) ON DELETE CASCADE,
 scope TEXT NOT NULL CHECK(scope IN ('repository:usage','task:usage')),
 repository_ref TEXT NOT NULL,
 repo_id INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
 task_ref TEXT NOT NULL DEFAULT '',
 session_id INTEGER REFERENCES sessions(id) ON DELETE CASCADE,
 PRIMARY KEY(grant_id,scope),
 CHECK((scope='repository:usage' AND task_ref='' AND session_id IS NULL) OR
       (scope='task:usage' AND task_ref<>'' AND session_id IS NOT NULL))
);
CREATE TRIGGER connector_usage_targets_immutable BEFORE UPDATE ON connector_usage_targets
BEGIN SELECT RAISE(ABORT, 'connector usage target is immutable'); END;
