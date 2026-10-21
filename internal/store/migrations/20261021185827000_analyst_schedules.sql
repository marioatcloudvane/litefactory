CREATE TABLE analyst_schedules (
 id TEXT PRIMARY KEY, creation_key TEXT NOT NULL UNIQUE, request_digest TEXT NOT NULL,
 repository_id INTEGER NOT NULL REFERENCES repos(id), version INTEGER NOT NULL,
 enabled INTEGER NOT NULL, next_due TEXT NOT NULL, record_json TEXT NOT NULL
);
CREATE INDEX analyst_schedules_due ON analyst_schedules(enabled,next_due,id);
CREATE TABLE analyst_schedule_versions (
 schedule_id TEXT NOT NULL REFERENCES analyst_schedules(id), version INTEGER NOT NULL,
 record_json TEXT NOT NULL, PRIMARY KEY(schedule_id,version)
);
CREATE TABLE analyst_occurrences (
 id INTEGER PRIMARY KEY AUTOINCREMENT, schedule_id TEXT NOT NULL REFERENCES analyst_schedules(id),
 repository_id INTEGER NOT NULL REFERENCES repos(id), slot TEXT NOT NULL,
 active INTEGER NOT NULL, record_json TEXT NOT NULL, UNIQUE(schedule_id,slot)
);
CREATE UNIQUE INDEX analyst_occurrences_schedule_active ON analyst_occurrences(schedule_id) WHERE active=1;
CREATE UNIQUE INDEX analyst_occurrences_repository_active ON analyst_occurrences(repository_id) WHERE active=1;
CREATE TABLE analyst_source_work (
 schedule_id TEXT NOT NULL REFERENCES analyst_schedules(id), generation INTEGER NOT NULL,
 source_key TEXT NOT NULL, occurrence_id INTEGER NOT NULL REFERENCES analyst_occurrences(id),
 record_json TEXT NOT NULL, PRIMARY KEY(schedule_id,generation,source_key)
);
CREATE INDEX analyst_source_occurrence ON analyst_source_work(occurrence_id,source_key);
CREATE TABLE analyst_proposal_notices (
 id INTEGER PRIMARY KEY AUTOINCREMENT, schedule_id TEXT NOT NULL REFERENCES analyst_schedules(id),
 occurrence_id INTEGER NOT NULL REFERENCES analyst_occurrences(id), proposal_id TEXT NOT NULL,
 proposal_version INTEGER NOT NULL, record_json TEXT NOT NULL,
 UNIQUE(occurrence_id,proposal_id,proposal_version),
 FOREIGN KEY(proposal_id,proposal_version) REFERENCES standards_proposal_versions(proposal_id,version)
);
CREATE TRIGGER analyst_config_immutable BEFORE UPDATE ON analyst_schedule_versions BEGIN SELECT RAISE(ABORT,'immutable schedule configuration'); END;
CREATE TRIGGER analyst_notice_immutable BEFORE UPDATE ON analyst_proposal_notices BEGIN SELECT RAISE(ABORT,'immutable proposal notice'); END;
CREATE INDEX analyst_recent_terminal ON runs(finished_at DESC,id DESC) WHERE status IN ('succeeded','failed','reported');
CREATE TRIGGER analyst_config_retained BEFORE DELETE ON analyst_schedule_versions BEGIN SELECT RAISE(ABORT,'retained schedule configuration'); END;
CREATE TRIGGER analyst_notice_retained BEFORE DELETE ON analyst_proposal_notices BEGIN SELECT RAISE(ABORT,'retained proposal notice'); END;
CREATE INDEX analyst_source_membership ON analyst_source_work(schedule_id,source_key);
