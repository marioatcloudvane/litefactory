# US-006 implementation report

Frozen issue #286; revision 389aaf43fe82fb26e76b590052f72f5c2e186496a7be46a124eb2bc4c7d3730f; dependency US-001 engineering completion supplied by orchestrator.

Implementation task complete. New criterion-derived tests and engineering submission remain orchestrator/test-specialist work. No review or acceptance claimed.

## Files

- `internal/turn/submissions.go`: original helper handler delegates to a shared checkpoint operation; retains existing session/progress/parent lock ordering, validation, snapshot capture and decisions.
- `internal/turn/evidence.go`: shared checkpoint preflight/submit/observation facade and EvidenceService with bounded public receipt output and observation-only recovery projection.

No US-001 production files changed.

## API handoff

`SubmissionSession.CheckpointBinding()` returns a copied factory checkpoint binding. Compose the result binding using the same run, task, attempt, submission version and workflow version. `NewEvidenceService(session, receipts)` refuses incompatible stages, attempt scope and owners. Implement supports engineering/legacy as already bound; Validate supports its existing assessment stage. Factory host composition remains #274.

`PreflightCheckpoint(ctx, payload)` returns shared `agent.ContributionDiagnostics`. `SubmitCheckpoint(ctx, requestID, payload)` preserves the helper's existing raw receipt-or-diagnostics response. `CheckpointReceipt` only observes retained current-attempt receipts. These methods recheck persisted checkpoint authority and do not select another mode.

`EvidenceService.Preflight(ctx, payload)` delegates shared validation and current snapshot checks without capturing or allocating domain proof. `HandleSubmit(ctx, requestID, payload)` repeats validation through the shared checkpoint engine and links the authoritative submission receipt; the public response is a bounded `FamilyReceipt`, excluding full payload/captured files. Pending checkpoint decisions are honestly uncertain with separate pending dimensions.

The strict tool input is **the existing agent.Contribution object itself**, including its stage/story/outcome and permitted engineering/assessment/capture/reference fields. There is no new wrapper or schema. Unknown fields, invalid stages, forbidden Validate capture and frozen/snapshot/reference faults remain owned by the existing strict parser and checkpoint validator.

US-002 dispatcher must acquire live AttemptGuard context and Begin the ledger with `tool=evidence_submit`, `payload=exact raw argument bytes`, before HandleSubmit. This is confirmed with the orchestrator. Include EvidenceService.PermissionMaterial in the composite immutable permission digest. Register `EvidenceReceiptProjection(db)` for both `submission` and `tool:evidence_submit`. The fallback uses the same stable request identity to observe a checkpoint if a crash lost its ledger link. No retained checkpoint yet produces empty dimensions with the ledger's existing pending/rejected disposition, rather than an artificial transport error. Lookup never records proof, recovers decisions or dispatches effects.

## Executed checks

- `go test ./internal/turn -run '^TestSubmission' -count=1`: PASS, `ok github.com/cloudvane/litefactory/internal/turn 12.831s`.
- `go vet ./internal/turn`: PASS, exit 0, no diagnostics.
- Follow-up targeted checkpoint regression command and output are returned inline to the orchestrator.

No tests authored, no new SQL, no forge effects, no mode transitions, no authentic host evidence. No implementation blocker.
