CREATE TABLE standards_exception_proposals (
 id TEXT PRIMARY KEY,
 operation_key TEXT NOT NULL UNIQUE,
 schema_version INTEGER NOT NULL CHECK(schema_version=1),
 digest TEXT NOT NULL,
 terms_json TEXT NOT NULL,
 created_at TEXT NOT NULL,
 owner TEXT NOT NULL,
 book_revision INTEGER NOT NULL,
 standard_id TEXT NOT NULL,
 repository_id INTEGER NOT NULL,
 expires_at TEXT NOT NULL,
 FOREIGN KEY(book_revision,standard_id) REFERENCES standards_standards(revision_id,id)
);
-- Repository identity is retained in terms. No repository foreign key prevents
-- deleting historical registration; a missing registration fails validation.
CREATE TABLE standards_exception_approvals (
 proposal_id TEXT PRIMARY KEY REFERENCES standards_exception_proposals(id),
 digest TEXT NOT NULL,
 owner TEXT NOT NULL,
 provenance TEXT NOT NULL CHECK(provenance='local-cli'),
 created_at TEXT NOT NULL
);
CREATE TABLE standards_exception_revocations (
 proposal_id TEXT PRIMARY KEY REFERENCES standards_exception_approvals(proposal_id),
 owner TEXT NOT NULL,
 provenance TEXT NOT NULL CHECK(provenance='local-cli'),
 reason TEXT NOT NULL,
 created_at TEXT NOT NULL
);
CREATE TRIGGER standards_exception_proposals_no_update BEFORE UPDATE ON standards_exception_proposals BEGIN SELECT RAISE(ABORT,'immutable standards exception'); END;
CREATE TRIGGER standards_exception_proposals_no_delete BEFORE DELETE ON standards_exception_proposals BEGIN SELECT RAISE(ABORT,'immutable standards exception'); END;
CREATE TRIGGER standards_exception_approvals_no_update BEFORE UPDATE ON standards_exception_approvals BEGIN SELECT RAISE(ABORT,'immutable standards exception approval'); END;
CREATE TRIGGER standards_exception_approvals_no_delete BEFORE DELETE ON standards_exception_approvals BEGIN SELECT RAISE(ABORT,'immutable standards exception approval'); END;
CREATE TRIGGER standards_exception_revocations_no_update BEFORE UPDATE ON standards_exception_revocations BEGIN SELECT RAISE(ABORT,'immutable standards exception revocation'); END;
CREATE TRIGGER standards_exception_revocations_no_delete BEFORE DELETE ON standards_exception_revocations BEGIN SELECT RAISE(ABORT,'immutable standards exception revocation'); END;
CREATE TRIGGER standards_exception_approval_binding BEFORE INSERT ON standards_exception_approvals WHEN NOT EXISTS (SELECT 1 FROM standards_exception_proposals WHERE id=NEW.proposal_id AND digest=NEW.digest AND created_at<=NEW.created_at AND NEW.created_at<expires_at) BEGIN SELECT RAISE(ABORT,'invalid standards exception approval'); END;
CREATE TRIGGER standards_exception_revocation_order BEFORE INSERT ON standards_exception_revocations WHEN NOT EXISTS (SELECT 1 FROM standards_exception_approvals WHERE proposal_id=NEW.proposal_id AND created_at<=NEW.created_at) BEGIN SELECT RAISE(ABORT,'invalid standards exception revocation'); END;
CREATE INDEX standards_exception_proposals_order ON standards_exception_proposals(created_at,id);
