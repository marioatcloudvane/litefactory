-- The owner side of Validate: the one live report comment, the owner's manual
-- verdicts and finding choices, and every remote effect the owner authorized
-- (issue, push, pull request, merge, close). All of them are bound to one
-- Validate run (the graph instance) and its tested commit. A verdict or choice
-- is replaced by a newer one, and the older one stays as history. A succeeded
-- effect never changes. Rows are removed only by archiving the whole task.

-- The single report comment of one Validate run.
CREATE TABLE validate_reports (
 instance_id INTEGER PRIMARY KEY CHECK(instance_id>0),
 comment_id INTEGER NOT NULL DEFAULT 0,
 marker TEXT NOT NULL DEFAULT '',
 revision TEXT NOT NULL DEFAULT '',
 digest TEXT NOT NULL DEFAULT '',
 notice TEXT NOT NULL DEFAULT '',
 updated_at TEXT NOT NULL
);

-- The owner's pass or fail for one criterion that needs a person.
CREATE TABLE validate_verdicts (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 instance_id INTEGER NOT NULL CHECK(instance_id>0),
 commit_sha TEXT NOT NULL CHECK(length(commit_sha) IN (40,64)),
 criterion TEXT NOT NULL CHECK(length(criterion)>0),
 verdict TEXT NOT NULL CHECK(verdict IN ('pass','fail')),
 evidence TEXT NOT NULL CHECK(length(evidence)>0),
 comment_id INTEGER NOT NULL CHECK(comment_id>0),
 author_id INTEGER NOT NULL CHECK(author_id>0),
 current INTEGER NOT NULL DEFAULT 1 CHECK(current IN (0,1)),
 created_at TEXT NOT NULL,
 UNIQUE(instance_id,comment_id,criterion)
);
CREATE UNIQUE INDEX validate_verdicts_current ON validate_verdicts(instance_id,commit_sha,criterion) WHERE current=1;
CREATE TRIGGER validate_verdicts_history BEFORE UPDATE ON validate_verdicts
WHEN OLD.current=0 OR NEW.current<>0 OR NEW.verdict<>OLD.verdict OR NEW.evidence<>OLD.evidence OR NEW.commit_sha<>OLD.commit_sha OR NEW.criterion<>OLD.criterion
BEGIN SELECT RAISE(ABORT,'a verdict is replaced by a newer one, never edited'); END;

-- The owner's one current choice for one finding.
CREATE TABLE validate_choices (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 instance_id INTEGER NOT NULL CHECK(instance_id>0),
 commit_sha TEXT NOT NULL CHECK(length(commit_sha) IN (40,64)),
 finding TEXT NOT NULL CHECK(length(finding)>0),
 choice TEXT NOT NULL CHECK(choice IN ('repair','issue','defer')),
 issue_kind TEXT NOT NULL DEFAULT '' CHECK(issue_kind IN ('','bug','story')),
 comment_id INTEGER NOT NULL CHECK(comment_id>0),
 author_id INTEGER NOT NULL CHECK(author_id>0),
 current INTEGER NOT NULL DEFAULT 1 CHECK(current IN (0,1)),
 created_at TEXT NOT NULL,
 CHECK((choice='issue')=(issue_kind<>'')),
 UNIQUE(instance_id,comment_id,finding)
);
CREATE UNIQUE INDEX validate_choices_current ON validate_choices(instance_id,commit_sha,finding) WHERE current=1;
CREATE TRIGGER validate_choices_history BEFORE UPDATE ON validate_choices
WHEN OLD.current=0 OR NEW.current<>0 OR NEW.choice<>OLD.choice OR NEW.issue_kind<>OLD.issue_kind OR NEW.commit_sha<>OLD.commit_sha OR NEW.finding<>OLD.finding
BEGIN SELECT RAISE(ABORT,'a choice is replaced by a newer one, never edited'); END;

-- One owner-authorized remote action. The intent is written before the remote
-- call. A succeeded effect is immutable.
CREATE TABLE validate_effects (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 instance_id INTEGER NOT NULL CHECK(instance_id>0),
 kind TEXT NOT NULL CHECK(kind IN ('issue','push','pr','merge','close')),
 commit_sha TEXT NOT NULL CHECK(length(commit_sha) IN (40,64)),
 target TEXT NOT NULL CHECK(length(target)>0),
 confirm_comment_id INTEGER NOT NULL DEFAULT 0,
 state TEXT NOT NULL CHECK(state IN ('intent','succeeded','failed','uncertain')),
 result TEXT NOT NULL DEFAULT '{}',
 error TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 UNIQUE(instance_id,kind,target)
);
CREATE TRIGGER validate_effects_succeeded_immutable BEFORE UPDATE ON validate_effects WHEN OLD.state='succeeded'
BEGIN SELECT RAISE(ABORT,'a succeeded effect is immutable'); END;
