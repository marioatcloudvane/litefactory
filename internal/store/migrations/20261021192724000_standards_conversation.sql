-- Coordination is deliberately independent of expiring chat body rows.
CREATE TABLE standards_conversation_contexts (
 transport TEXT NOT NULL, owner_id TEXT NOT NULL, channel_id TEXT NOT NULL,
 version INTEGER NOT NULL CHECK(version>0), record_json TEXT NOT NULL,
 PRIMARY KEY(transport,owner_id,channel_id)
);
CREATE TABLE standards_conversation_operations (
 operation_key TEXT PRIMARY KEY, record_json TEXT NOT NULL
);
CREATE TABLE standards_conversation_reviews (
 id TEXT PRIMARY KEY, transport TEXT NOT NULL, owner_id TEXT NOT NULL, channel_id TEXT NOT NULL, code TEXT NOT NULL, record_json TEXT NOT NULL,
 UNIQUE(transport,owner_id,channel_id,code)
);
CREATE TABLE standards_conversation_notice_deliveries (
 notice_id INTEGER NOT NULL, transport TEXT NOT NULL, owner_id TEXT NOT NULL,
 channel_id TEXT NOT NULL, status TEXT NOT NULL, next_attempt TEXT,
 record_json TEXT NOT NULL,
 PRIMARY KEY(notice_id,transport,owner_id,channel_id)
);
CREATE INDEX standards_conversation_notice_pending ON standards_conversation_notice_deliveries(status,next_attempt,notice_id);

-- Preserve immutable exception history while allowing authenticated Discord attribution.
CREATE TEMP TABLE standards_exception_approval_backup AS SELECT * FROM standards_exception_approvals;
CREATE TEMP TABLE standards_exception_revocation_backup AS SELECT * FROM standards_exception_revocations;
DROP TRIGGER standards_exception_approvals_no_delete;
DROP TRIGGER standards_exception_revocations_no_delete;
DROP TABLE standards_exception_revocations;
DROP TABLE standards_exception_approvals;
CREATE TABLE standards_exception_approvals (
 proposal_id TEXT PRIMARY KEY REFERENCES standards_exception_proposals(id),
 digest TEXT NOT NULL,
 owner TEXT NOT NULL,
 provenance TEXT NOT NULL CHECK(provenance='local-cli' OR provenance GLOB 'discord-owner:[0-9]*:[0-9]*:[0-9]*'),
 created_at TEXT NOT NULL
);
CREATE TABLE standards_exception_revocations (
 proposal_id TEXT PRIMARY KEY REFERENCES standards_exception_approvals(proposal_id),
 owner TEXT NOT NULL,
 provenance TEXT NOT NULL CHECK(provenance='local-cli' OR provenance GLOB 'discord-owner:[0-9]*:[0-9]*:[0-9]*'),
 reason TEXT NOT NULL,
 created_at TEXT NOT NULL
);
INSERT INTO standards_exception_approvals SELECT * FROM standards_exception_approval_backup;
INSERT INTO standards_exception_revocations SELECT * FROM standards_exception_revocation_backup;
DROP TABLE standards_exception_approval_backup;
DROP TABLE standards_exception_revocation_backup;
CREATE TRIGGER standards_exception_approvals_no_update BEFORE UPDATE ON standards_exception_approvals BEGIN SELECT RAISE(ABORT,'immutable standards exception approval'); END;
CREATE TRIGGER standards_exception_approvals_no_delete BEFORE DELETE ON standards_exception_approvals BEGIN SELECT RAISE(ABORT,'immutable standards exception approval'); END;
CREATE TRIGGER standards_exception_revocations_no_update BEFORE UPDATE ON standards_exception_revocations BEGIN SELECT RAISE(ABORT,'immutable standards exception revocation'); END;
CREATE TRIGGER standards_exception_revocations_no_delete BEFORE DELETE ON standards_exception_revocations BEGIN SELECT RAISE(ABORT,'immutable standards exception revocation'); END;
CREATE TRIGGER standards_exception_approval_binding BEFORE INSERT ON standards_exception_approvals WHEN NOT EXISTS (SELECT 1 FROM standards_exception_proposals WHERE id=NEW.proposal_id AND digest=NEW.digest AND created_at<=NEW.created_at AND NEW.created_at<expires_at) BEGIN SELECT RAISE(ABORT,'invalid standards exception approval'); END;
CREATE TRIGGER standards_exception_revocation_order BEFORE INSERT ON standards_exception_revocations WHEN NOT EXISTS (SELECT 1 FROM standards_exception_approvals WHERE proposal_id=NEW.proposal_id AND created_at<=NEW.created_at) BEGIN SELECT RAISE(ABORT,'invalid standards exception revocation'); END;
