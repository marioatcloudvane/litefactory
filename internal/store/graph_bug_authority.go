package store

import "errors"

// ErrBugRepairUnauthorized indicates missing or inapplicable diagnosis authority.
var ErrBugRepairUnauthorized = errors.New("store: Bug repair lacks current exact approval")

// BugIdentity is observed factory attribution, never agent-authored authority.
type BugIdentity struct {
	SessionID                     int64
	Repository                    string
	IssueID, IssueNumber, OwnerID int64
}

// Historical Bug sessions are kept outside automatic orphan repair. Their owner
// will repost separately; startup must not reinterpret or reclassify old work.
const unsupportedLegacyBugSessions = `(SELECT session_id FROM bug_tasks UNION SELECT i.session_id FROM graph_instances i JOIN graph_definitions d ON d.id=i.definition_id WHERE d.workflow='bug' AND d.profile_version<>2)`
