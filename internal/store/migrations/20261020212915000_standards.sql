CREATE TABLE standards_revisions (
 id INTEGER PRIMARY KEY, parent_revision INTEGER REFERENCES standards_revisions(id),
 schema_version INTEGER NOT NULL, digest TEXT NOT NULL, created_at TEXT NOT NULL
);
CREATE TABLE standards_book (id INTEGER PRIMARY KEY CHECK(id=1), current_revision INTEGER REFERENCES standards_revisions(id));
INSERT INTO standards_book(id) VALUES(1);
CREATE TABLE standards_categories (
 revision_id INTEGER NOT NULL REFERENCES standards_revisions(id), id TEXT NOT NULL,
 name TEXT NOT NULL, description TEXT NOT NULL, PRIMARY KEY(revision_id,id), UNIQUE(revision_id,name)
);
CREATE TABLE standards_standards (
 revision_id INTEGER NOT NULL REFERENCES standards_revisions(id), id TEXT NOT NULL,
 title TEXT NOT NULL, body TEXT NOT NULL, PRIMARY KEY(revision_id,id)
);
CREATE TABLE standards_memberships (
 revision_id INTEGER NOT NULL, standard_id TEXT NOT NULL, category_id TEXT NOT NULL,
 PRIMARY KEY(revision_id,standard_id,category_id),
 FOREIGN KEY(revision_id,standard_id) REFERENCES standards_standards(revision_id,id),
 FOREIGN KEY(revision_id,category_id) REFERENCES standards_categories(revision_id,id)
);
CREATE TABLE standards_proposals (id TEXT PRIMARY KEY, current_version INTEGER NOT NULL, published_version INTEGER);
CREATE TABLE standards_proposal_versions (
 proposal_id TEXT NOT NULL REFERENCES standards_proposals(id), version INTEGER NOT NULL,
 base_revision INTEGER REFERENCES standards_revisions(id), schema_version INTEGER NOT NULL,
 operations_json TEXT NOT NULL, change_digest TEXT NOT NULL, result_digest TEXT NOT NULL,
 author TEXT NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(proposal_id,version)
);
CREATE TABLE standards_approvals (
 proposal_id TEXT NOT NULL, version INTEGER NOT NULL, base_revision INTEGER REFERENCES standards_revisions(id),
 change_digest TEXT NOT NULL,result_digest TEXT NOT NULL,owner TEXT NOT NULL,provenance TEXT NOT NULL,created_at TEXT NOT NULL,
 PRIMARY KEY(proposal_id,version), FOREIGN KEY(proposal_id,version) REFERENCES standards_proposal_versions(proposal_id,version)
);
CREATE TABLE standards_publications (
 revision_id INTEGER PRIMARY KEY REFERENCES standards_revisions(id), proposal_id TEXT NOT NULL,version INTEGER NOT NULL,
 base_revision INTEGER REFERENCES standards_revisions(id),change_digest TEXT NOT NULL,result_digest TEXT NOT NULL,
 owner TEXT NOT NULL,provenance TEXT NOT NULL,created_at TEXT NOT NULL, UNIQUE(proposal_id,version),
 FOREIGN KEY(proposal_id,version) REFERENCES standards_approvals(proposal_id,version)
);
CREATE TRIGGER standards_revisions_immutable_update BEFORE UPDATE ON standards_revisions BEGIN SELECT RAISE(ABORT, 'immutable standards record'); END;
CREATE TRIGGER standards_revisions_immutable_delete BEFORE DELETE ON standards_revisions BEGIN SELECT RAISE(ABORT, 'immutable standards record'); END;
CREATE TRIGGER standards_categories_immutable_update BEFORE UPDATE ON standards_categories BEGIN SELECT RAISE(ABORT, 'immutable standards record'); END;
CREATE TRIGGER standards_categories_immutable_delete BEFORE DELETE ON standards_categories BEGIN SELECT RAISE(ABORT, 'immutable standards record'); END;
CREATE TRIGGER standards_standards_immutable_update BEFORE UPDATE ON standards_standards BEGIN SELECT RAISE(ABORT, 'immutable standards record'); END;
CREATE TRIGGER standards_standards_immutable_delete BEFORE DELETE ON standards_standards BEGIN SELECT RAISE(ABORT, 'immutable standards record'); END;
CREATE TRIGGER standards_memberships_immutable_update BEFORE UPDATE ON standards_memberships BEGIN SELECT RAISE(ABORT, 'immutable standards record'); END;
CREATE TRIGGER standards_memberships_immutable_delete BEFORE DELETE ON standards_memberships BEGIN SELECT RAISE(ABORT, 'immutable standards record'); END;
CREATE TRIGGER standards_proposal_versions_immutable_update BEFORE UPDATE ON standards_proposal_versions BEGIN SELECT RAISE(ABORT, 'immutable standards record'); END;
CREATE TRIGGER standards_proposal_versions_immutable_delete BEFORE DELETE ON standards_proposal_versions BEGIN SELECT RAISE(ABORT, 'immutable standards record'); END;
CREATE TRIGGER standards_approvals_immutable_update BEFORE UPDATE ON standards_approvals BEGIN SELECT RAISE(ABORT, 'immutable standards record'); END;
CREATE TRIGGER standards_approvals_immutable_delete BEFORE DELETE ON standards_approvals BEGIN SELECT RAISE(ABORT, 'immutable standards record'); END;
CREATE TRIGGER standards_publications_immutable_update BEFORE UPDATE ON standards_publications BEGIN SELECT RAISE(ABORT, 'immutable standards record'); END;
CREATE TRIGGER standards_publications_immutable_delete BEFORE DELETE ON standards_publications BEGIN SELECT RAISE(ABORT, 'immutable standards record'); END;
CREATE TRIGGER standards_categories_sealed BEFORE INSERT ON standards_categories
WHEN EXISTS(SELECT 1 FROM standards_publications WHERE revision_id=NEW.revision_id)
BEGIN SELECT RAISE(ABORT, 'published standards snapshot is sealed'); END;
CREATE TRIGGER standards_standards_sealed BEFORE INSERT ON standards_standards
WHEN EXISTS(SELECT 1 FROM standards_publications WHERE revision_id=NEW.revision_id)
BEGIN SELECT RAISE(ABORT, 'published standards snapshot is sealed'); END;
CREATE TRIGGER standards_memberships_sealed BEFORE INSERT ON standards_memberships
WHEN EXISTS(SELECT 1 FROM standards_publications WHERE revision_id=NEW.revision_id)
BEGIN SELECT RAISE(ABORT, 'published standards snapshot is sealed'); END;
