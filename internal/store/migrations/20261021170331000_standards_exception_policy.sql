CREATE TABLE standards_exception_policies (
 revision_id INTEGER NOT NULL,
 standard_id TEXT NOT NULL,
 policy_json TEXT NOT NULL,
 PRIMARY KEY(revision_id,standard_id),
 FOREIGN KEY(revision_id,standard_id) REFERENCES standards_standards(revision_id,id)
);
CREATE TRIGGER standards_exception_policies_immutable_update BEFORE UPDATE ON standards_exception_policies
BEGIN SELECT RAISE(ABORT, 'immutable standards record'); END;
CREATE TRIGGER standards_exception_policies_immutable_delete BEFORE DELETE ON standards_exception_policies
BEGIN SELECT RAISE(ABORT, 'immutable standards record'); END;
CREATE TRIGGER standards_exception_policies_sealed BEFORE INSERT ON standards_exception_policies
WHEN EXISTS(SELECT 1 FROM standards_publications WHERE revision_id=NEW.revision_id)
BEGIN SELECT RAISE(ABORT, 'published standards snapshot is sealed'); END;
