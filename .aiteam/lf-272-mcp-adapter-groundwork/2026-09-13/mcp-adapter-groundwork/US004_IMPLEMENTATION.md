# US-004 production implementation — run 0007

Story #278 / 5442842977; revision 7fe2d1453415e2988eb510de1e1b48d47e6130544a6ab30da74b744d5eb07b2c; dependencies US-001/US-002 recorded. Owner reversal places this foundation before #264 lifecycle/control wiring.

Created: internal/submission/creation.go; internal/store/mcp_creation.go; internal/store/migrations/0043_mcp_creation.sql; internal/turn/mcp_creation.go; internal/daemon/mcp_creation.go.
Modified: internal/config/{config,template}.go; internal/store/mcp_authority.go; docs/mcp-adapter.md.

The canonical version-1 envelope fixes repository/source, linked/attach context and exact per-artifact titles/bodies/parent/link targets. Artifact count is derived from its array, checked again against retained bytes in store and capped at five. SHA256 is recomputed from canonical bytes. No model hash or IDs alone authorize an operation.

CreationAuthorizer copies loaded gate and configured numeric owner/bot identities at construction. Exact whole-command parsing and trusted daemon github.Comment/source metadata feed immutable intents. A proposal must have existed at the approval timestamp and match current source/relationship. Comment IDs deduplicate immutably; changed proposals advance retained revision, so reverting a hash cannot revive old approval. No MCP grant mint/read tool exists, and the initial catalog still contains no creation handler.

Store extends the existing MCP lease with bound_ns, without creating another attempt identity. Grants require a next eligible live Research launch after approval and expire at min(comment-created +30m, attempt expiry). Missing, foreign, replaced/restarted, expired and content-mismatched authority refuses before replay/reservation/dispatch. Time is freshly sampled within transactions; NewCreationStore(db, now) provides an explicit factory/test bookkeeping seam (nil means real store clock).

ReserveCreation runs one short transaction with authority, content, grant and quota checks. Fixed conservative limits: one batch/five artifacts per attempt and twenty per repository rolling 24h. Pending/uncertain/unreleased failures stay charged irrespective of age; confirmed batches count within their window. First admission returns one internal nonserializable CreationPermit; duplicates return the same durable CreationOperation without a permit. BeginDispatch consumes that owner once after fresh checks, before external I/O. No transaction spans I/O.

Outcome/recovery APIs preserve reserved/dispatched/confirmed/uncertain/failed/released states without renewed agent authority. Definitively never-dispatched reserved/failed work may release quota without reopening its grant. Confirmed/released outcomes cannot be reversed; ambiguous work cannot obtain another dispatcher. The primitive does not claim remote exactly-once effects.

Public seams: submission.CanonicalCreation; store.NewCreationStore and its RetainProposal/AdmitOwnerIntent/BindGrant/ReserveCreation/BeginDispatch/RecordOperationOutcome/ReconcileOperation/GetCreationOperation; turn.NewCreationAuthorizer and corresponding guarded facade; daemon.AdmitMCPOwnerCommand. All approval/grant/recovery methods are trusted factory-only APIs. #273 owns real handlers and external effects; #264 will connect control/launch adoption; #274 owns authentic host isolation. No control polling or actual publication is implemented here.

Executed scoped compatibility command:
`go test ./internal/config ./internal/submission ./internal/store ./internal/turn ./internal/daemon -run 'TestMCPAutoStart_|TestMCPTools_|TestMCPLease_|TestMCPAttempt_|TestMCPLaunch_|TestDefaultFileAutoResumeOnSessionLimit' -count=1`

Executed `go vet ./internal/store ./internal/submission ./internal/turn ./internal/daemon ./internal/config`: passed without output.

New US004 unit and real SQLite grant/quota/fault tests remain required from test specialists. No full suites, acceptance review, issue closure, external forge calls, daemon state changes, commits or pushes.

Final compatibility output: config 0.585s; submission 0.690s [no tests to run]; store 1.900s; turn 3.660s; daemon 0.922s [no tests to run]. All passed.
