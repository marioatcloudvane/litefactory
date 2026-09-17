targeted family profile feedback socket comment and pinned-surface race retest
go test -race -cover ./internal/submission ./internal/turn ./internal/github -run (TestFamilyProfile_|TestFeedbackUnit_|TestFamilySocket|TestFeedbackSocket|TestIssueComment|TestIssuePublicationService_|TestClientMethodSetIsPinned) -count=1
ok github.com/cloudvane/litefactory/internal/submission 1.475s coverage: 9.9%
ok github.com/cloudvane/litefactory/internal/turn 28.011s coverage: 9.4%
ok github.com/cloudvane/litefactory/internal/github 2.191s coverage: 0.0%
Zero failures; invalid raw UTF-8 regression passes.

go test -race ./internal/turn ./internal/store ./internal/github -run ^(TestCommentHTTP|TestCommentReservation|TestCommentPage) -count=1
ok internal/turn 29.578s
ok internal/store 4.693s
ok internal/github 2.203s
Seven tests pass. Concurrent retries one POST; redaction bounds; refusals; creation-gate independence; lost replies/reopen/cancellation; authenticated exact reconciliation; ambiguous observations; no publishing by lookup; bounded pages and cross-connection one owner. SQLite BUSY/BUSY_SNAPSHOT are fail-closed transient errors, followed by identical retry with no new dispatch right.
Pinned GitHub surface including later ListIssuesPage addition also passed US003 scoped check.
