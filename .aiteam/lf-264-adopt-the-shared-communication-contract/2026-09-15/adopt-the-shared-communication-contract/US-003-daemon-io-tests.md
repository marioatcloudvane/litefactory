# US-003 daemon I/O execution

Frozen identity: issue #300 / 5463752385, parent 5439211198, revision `5f7bc6777358833a0b5af6923475849980c79ce1555b8644a7b0cc8153a5fef5`; dependencies US-001/US-002. Workflow 2 engineering only.

## Result

Three tests passed, zero final failures. Owned file: `internal/daemon/publication_recovery_io_internal_test.go`. No production changes.

- `TestPublicationDaemonIO_TrustedPreparationApproval`: real `handleComment` intake, numeric identity filter, `handleMCPCreation`, redacted exact proposal presentation using real reporter and controlled HTTP; no preparation grant; deterministic replay retains presented hash; copied approval under an owner display name with foreign numeric ID grants nothing; exact owner approval and replay preserve one retained intent. Source link survives presentation.
- `TestPublicationDaemonIO_CommentRestartBoundedPages`: actual comment service dispatch loses a successful remote response; exact redacted uncertain intent persists with no fabricated comment ID; original attempt ends and SQLite/runtime reopen; tick-owned recovery follows an empty page's next link, stays uncertain before the matching page, suppresses early ticks, and confirms numeric ID 991 from the second page. Exactly one POST survives restart.
- `TestPublicationDaemonIO_CreationRestartOpenClosedPages`: actual guarded creation service dispatch loses its response; retained receipt remains uncertain with zero remote identity; SQLite/runtime restart and attempt ending preserve intent. With new creation disabled, tick-owned observation visits open:1, open:2, closed:1 and confirms the exact issue ID 8877 / number 888 and linked relationship. Exactly one creation POST; no redispatch; redacted body preserved.

The environment uses repository modernc SQLite in isolated temporary paths and in-process httptest HTTP with the real GitHub client. No environment service URLs, credentials, live forge, daemon state, nested provider, build tags, or test sleeps are used. Page scheduling uses explicit time arguments. Factory startup construction and full daemon Tick discovery are outside this file; actual production services are composed into the existing daemon turnExecutor and its polling/intake seams. Broader constructor/catalog and HTTP refusal coverage belongs to the other US-003 specialists.

## Executed checks

Base HEAD: `16634ed33075ea3acfc449bcb6316163bdc0b807`, with the shared uncommitted US-001/US-002/US-003 implementation. Final immutable implementation digest is the orchestrator's capture responsibility.

1. `go test ./internal/daemon -run '^TestPublicationDaemonIO_' -count=1 -timeout=8m`

   `ok  github.com/cloudvane/litefactory/internal/daemon  0.701s`

2. `go test -race ./internal/daemon -run '^TestPublicationDaemonIO_' -count=1 -timeout=8m`

   `ok  github.com/cloudvane/litefactory/internal/daemon  7.577s`

3. `gofmt -l internal/daemon/publication_recovery_io_internal_test.go`

   Exit 0, no output.

The final race run includes the final authority-cleanup addition. Earlier plain runs passed at 0.648s and 0.799s; an earlier race run passed at 7.350s. During test construction two fixture assumptions failed and were corrected: the mode label must be `Mode: Research`, and an uncertain creation receipt legitimately contains a pending publication item with zero numeric identity. Neither exposed a production defect. No full package/repository test, make check, acceptance, or authentic host observation was performed. External #274/#296 proof remains unsatisfied.
