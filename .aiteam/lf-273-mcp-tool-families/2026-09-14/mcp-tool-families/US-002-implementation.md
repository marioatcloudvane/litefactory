# US-002 implementation report

Frozen issue #282; revision 77c0f77cfe929db441d3bc020bb0d1f0434f134d78e768c59e668c83b549ff65. Dependencies US-001 and US-006 supplied engineering-complete. Implementation task complete; new criterion-derived tests and engineering capture remain orchestrator/test-specialist work. No acceptance assessment.

## Owned files

Modified `internal/submission/tools.go` and `internal/turn/mcp_tools.go`. Created `internal/submission/family_tools.go`, `internal/turn/feedback.go`, `internal/turn/mcp_families.go`. No prior-story captured source paths changed.

## Composition

Keep `NewToolDispatcher` and `NewToolProfile` for the original communication-only contract. New `FamilyDispatcherOptions` holds Service, Receipts, Feedback, Evidence, AllowCreation, CommentTargets and Registrations. Call `FamilyPermissionDigest(options)` before `AttemptAuthority.Bind`, then `NewFamilyToolDispatcher(guard, options)` with the same options. Constructor compares the complete immutable digest and binding, validates every registration and requires all automatically advertised dependencies.

`NewFeedbackService(application, evidence)` requires shared evidence in Implement/Validate, and nil evidence in Research/Plan/Spike. Register `EvidenceReceiptProjection(db)` under both `submission` and `tool:evidence_submit` when constructing receipts for Implement/Validate. Evidence and feedback must use the same evidence instance; evidence must use the same receipt owner. Receipt historical scopes/projections, full checkpoint material, application grants, catalog version, creation gate, sorted unique positive CommentTargets and additional handler permission materials enter the digest. Comment target metadata is bounded to 100 immutable entries; actual comment authorization remains the publication owner's job.

Feedback arguments are strictly `{"kind":"application|checkpoint|creation","input":<existing typed object>}`. Application and checkpoint return their existing shared diagnostics. Creation delegates canonical validation to `submission.CanonicalCreation`, checks the immutable source/repository, and returns `{valid,diagnostics:[]}`. It does not retain proposals, grant authority or capture proof. Submit owners retain their own repeated shared validation.

Incremental services implement `FamilyOwner` (`Binding() store.ResultBinding`, `PermissionMaterial() ([]byte,error)` with valid JSON) and install `FamilyRegistration{Name, Owner, Handle}`. Handle signature is `(context.Context, string, []byte) (json.RawMessage,error)` with exact arguments and stable request ID. Only known family names are accepted, duplicate registrations refuse, no custom effect classification is accepted. Caller parameters cannot register tools or change permissions. Core feedback/receipt/evidence registrations are automatic and cannot be overridden.

The dispatcher obtains `AttemptGuard.Context` before discovery or call handling. Authenticated family calls Begin the durable ledger with exact argument bytes before profile/argument refusal; malformed recoverable envelopes are correlation-only and never authorize handlers. Unknown tools and forbidden modes get rejected dispositions. Receipts_get never Begins a ledger record and reads only its argument/request correlation. Communication identity/discovery/helper behavior remains unchanged.

The dispatcher invokes each family handler only on the ledger's first invocation. Duplicate calls return `FamilyReceipt`, not a replay of potentially large read/preflight output. For creation, a mandatory registration `ReplayCheck(ctx,requestID,args) error` freshly checks current primitive grant authorization on every duplicate before receipt observation; it must not publish or reserve a new dispatch. Handlers own strict fields, business validation, durable outcome references, and validation-error disposition. Finish their ledger reference before returning when there is domain outcome truth. Otherwise dispatcher marks successful pending calls confirmed, read/routing errors rejected, and unclassified effect errors uncertain. Cancellation propagates; unresolved bookkeeping remains recoverable. Comment/creation reconciliation is a separately authorized observation API, never implicit handler redispatch.

## Installed-profile matrix

| Catalog entry | Research | Implement | Validate | Plan/Spike |
|---|---|---|---|---|
| Existing communication trio | Yes | Yes | Yes | Yes, unchanged |
| feedback_preflight, receipts_get | Yes | Yes | Yes | Hidden/refused |
| evidence_submit | Hidden/refused | Yes | Yes | Hidden/refused |
| Registered issues_list/get/comment, activity_update/get | Yes | Yes | Yes | Hidden/refused |
| Registered issues_create_batch | Only with AllowCreation true; live primitive grant still required | Hidden/refused | Hidden/refused | Hidden/refused |

AllowCreation defaults false; registration never enables it. Missing incremental handlers are not advertised. A supplied registration missing its owner, handler, matching binding or valid permission material fails construction, including creation without ReplayCheck. Later US-008 proves the complete installed matrix; production host construction remains #274.

## Actual executed checks

`go test ./internal/submission ./internal/turn -run 'TestMCP|TestToolProfile' -count=1` passed:

```
ok github.com/cloudvane/litefactory/internal/submission 0.525s
ok github.com/cloudvane/litefactory/internal/turn 4.542s
```

`go vet ./internal/submission ./internal/turn` passed, exit 0, no diagnostics. Owned Go files gofmt applied. No tests authored, no full suite run, no forge/publication effects and no engineering submission from this specialist.
