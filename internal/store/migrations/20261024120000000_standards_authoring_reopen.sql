-- A reopened authoring session completes again into the next version of the
-- same proposal, so its completion link may move forward, never sideways.
DROP TRIGGER standards_authoring_completions_immutable_update;
CREATE TRIGGER standards_authoring_completions_forward_update BEFORE UPDATE ON standards_authoring_completions WHEN NEW.session_id!=OLD.session_id OR NEW.proposal_id!=OLD.proposal_id OR NEW.version<=OLD.version OR NEW.proposal_version<=OLD.proposal_version BEGIN SELECT RAISE(ABORT, 'authoring completion only moves forward'); END;
