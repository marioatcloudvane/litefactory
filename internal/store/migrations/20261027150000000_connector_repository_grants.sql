-- Task-level and repository-level connector access covers repositories, not
-- single bound tasks; the client names the task on each call. A grant either
-- covers all registered repositories (including later registrations) or the
-- repositories listed in connector_grant_repositories.
ALTER TABLE connector_grants ADD COLUMN all_repositories INTEGER NOT NULL DEFAULT 0 CHECK(all_repositories IN (0,1));
CREATE TABLE connector_grant_repositories (
 grant_id TEXT NOT NULL REFERENCES connector_grants(id) ON DELETE CASCADE,
 repo_id INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
 repository_ref TEXT NOT NULL,
 PRIMARY KEY(grant_id,repo_id)
);
CREATE TRIGGER connector_grant_repositories_immutable BEFORE UPDATE ON connector_grant_repositories
BEGIN SELECT RAISE(ABORT, 'connector repository coverage is immutable'); END;
-- Existing grants bound to one task or repository keep working: each now
-- covers the repository of its former binding.
INSERT OR IGNORE INTO connector_grant_repositories(grant_id,repo_id,repository_ref)
 SELECT grant_id,repo_id,repository_ref FROM connector_grant_targets;
INSERT OR IGNORE INTO connector_grant_repositories(grant_id,repo_id,repository_ref)
 SELECT grant_id,repo_id,repository_ref FROM connector_usage_targets;
INSERT OR IGNORE INTO connector_grant_repositories(grant_id,repo_id,repository_ref)
 SELECT t.grant_id,t.repo_id,r.forge||':'||r.owner||'/'||r.name FROM connector_code_targets t JOIN repos r ON r.id=t.repo_id;
DROP TABLE connector_code_targets;
DROP TABLE connector_usage_targets;
DROP TABLE connector_grant_targets;
