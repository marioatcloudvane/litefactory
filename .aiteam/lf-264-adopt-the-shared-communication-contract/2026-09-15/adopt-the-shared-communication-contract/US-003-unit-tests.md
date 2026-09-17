# US-003 unit engineering evidence

Story: US-003, issue #300, issue ID 5463752385, parent ID 5439211198.
Frozen revision: `5f7bc6777358833a0b5af6923475849980c79ce1555b8644a7b0cc8153a5fef5`.
Dependencies: US-001, US-002. Workflow: 2; engineering execution only.

## Executed state and scope

Executed on 2026-09-15 in the task-264 worktree, base HEAD
`16634ed33075ea3acfc449bcb6316163bdc0b807` plus the current uncommitted
US-001/002/003 implementation and these test changes. The orchestrator must bind
the final immutable file manifest to the aggregate implementation digest; HEAD
alone does not identify these working changes.

Added test files:

- `internal/turn/publication_controls_unit_test.go`
- `internal/report/creation_reply_unit_test.go`

Four new test functions with 32 passing subtests; zero failures. The same focused
run also passed four existing test functions for approval grammar, untrusted
control intake, exact-envelope decoding and permission material.

AC-002/003: preparation refuses non-owner/bot identities, edited/missing
provenance, disabled/missing authorization, all non-Research modes, unrelated
repository, quoted/prose commands, unsupported versions, malformed/trailing JSON
and caller authority fields before persistence. Existing tests check exact
linked/attach approval syntax and exact reviewed-envelope scope refusals.

AC-002/005: owner preview preserves complete JSON (including source links and
relationships) and the exact hash/relationship approval command for linked and
attach. Wrong fingerprints, modified bodies, malformed JSON, unsupported
relationships, oversized/truncated previews and redaction changes return an error
and no approvable output.

## Command and actual output

```text
go test ./internal/turn ./internal/report -run 'TestMCPRuntime_PrepareCreationProposal|TestReporter_CreationProposal|TestOwnerGrantCommand_ExactPublicGrammar|TestCreationAuthorizer_RejectsUntrustedControlBeforeStore|TestCreationPublicationService_DecodeExactReviewedEnvelope|TestCreationPublicationService_PermissionMaterialExcludesPermit' -short -race -cover -timeout=8m
ok  	github.com/cloudvane/litefactory/internal/turn	2.516s	coverage: 0.6% of statements
ok  	github.com/cloudvane/litefactory/internal/report	2.493s	coverage: 5.9% of statements
```

Exit status 0. Coverage figures are whole-package denominators for this focused
selection, not coverage claims for the complete packages.

```text
gofmt -l internal/turn/publication_controls_unit_test.go internal/report/creation_reply_unit_test.go
```

Exit status 0; no output.

## Remaining evidence boundaries

Valid proposal preparation, retained exact matching approvals, next-attempt
binding, actual production catalog/digest grants, and durable dispatch/restart
reconciliation use concrete database owners and require the separate I/O
specialist's tests (AC-001, remaining AC-002/003, AC-004/005). These unit tests
perform no database, HTTP, filesystem or provider I/O. No authentic host behavior,
forge delivery, acceptance, issue closure or whole-feature completion is claimed.
