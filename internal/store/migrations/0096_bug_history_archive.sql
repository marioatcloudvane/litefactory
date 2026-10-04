-- Preserve surviving legacy Bug history in place. SQLite refusal triggers also
-- abort ancestor cascades, rolling the entire deletion back.
CREATE TRIGGER archive_bug_tasks_insert BEFORE INSERT ON bug_tasks
BEGIN SELECT RAISE(ABORT, 'legacy Bug history is read-only'); END;
CREATE TRIGGER archive_bug_tasks_update BEFORE UPDATE ON bug_tasks
BEGIN SELECT RAISE(ABORT, 'legacy Bug history is read-only'); END;
CREATE TRIGGER archive_bug_tasks_delete BEFORE DELETE ON bug_tasks
BEGIN SELECT RAISE(ABORT, 'legacy Bug history is read-only'); END;
CREATE TRIGGER archive_bug_analyses_insert BEFORE INSERT ON bug_analyses
BEGIN SELECT RAISE(ABORT, 'legacy Bug history is read-only'); END;
CREATE TRIGGER archive_bug_analyses_update BEFORE UPDATE ON bug_analyses
BEGIN SELECT RAISE(ABORT, 'legacy Bug history is read-only'); END;
CREATE TRIGGER archive_bug_analyses_delete BEFORE DELETE ON bug_analyses
BEGIN SELECT RAISE(ABORT, 'legacy Bug history is read-only'); END;
CREATE TRIGGER archive_bug_repairs_insert BEFORE INSERT ON bug_repairs
BEGIN SELECT RAISE(ABORT, 'legacy Bug history is read-only'); END;
CREATE TRIGGER archive_bug_repairs_update BEFORE UPDATE ON bug_repairs
BEGIN SELECT RAISE(ABORT, 'legacy Bug history is read-only'); END;
CREATE TRIGGER archive_bug_repairs_delete BEFORE DELETE ON bug_repairs
BEGIN SELECT RAISE(ABORT, 'legacy Bug history is read-only'); END;
CREATE TRIGGER archive_bug_checks_insert BEFORE INSERT ON bug_checks
BEGIN SELECT RAISE(ABORT, 'legacy Bug history is read-only'); END;
CREATE TRIGGER archive_bug_checks_update BEFORE UPDATE ON bug_checks
BEGIN SELECT RAISE(ABORT, 'legacy Bug history is read-only'); END;
CREATE TRIGGER archive_bug_checks_delete BEFORE DELETE ON bug_checks
BEGIN SELECT RAISE(ABORT, 'legacy Bug history is read-only'); END;
CREATE TRIGGER archive_bug_delivery_insert BEFORE INSERT ON bug_delivery
BEGIN SELECT RAISE(ABORT, 'legacy Bug history is read-only'); END;
CREATE TRIGGER archive_bug_delivery_update BEFORE UPDATE ON bug_delivery
BEGIN SELECT RAISE(ABORT, 'legacy Bug history is read-only'); END;
CREATE TRIGGER archive_bug_delivery_delete BEFORE DELETE ON bug_delivery
BEGIN SELECT RAISE(ABORT, 'legacy Bug history is read-only'); END;
CREATE TRIGGER archive_bug_events_insert BEFORE INSERT ON bug_events
BEGIN SELECT RAISE(ABORT, 'legacy Bug history is read-only'); END;
CREATE TRIGGER archive_bug_events_update BEFORE UPDATE ON bug_events
BEGIN SELECT RAISE(ABORT, 'legacy Bug history is read-only'); END;
CREATE TRIGGER archive_bug_events_delete BEFORE DELETE ON bug_events
BEGIN SELECT RAISE(ABORT, 'legacy Bug history is read-only'); END;
