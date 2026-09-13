# US-001 unit execution

Frozen revision: aa0b9b25c124b7705a076270467175ed58256758bebf3fd57a9c777c9591d433. Engineering test evidence only; no acceptance assessment.

Six test functions passed, zero failed. External dependencies are in-memory fakes; no live forge, database, filesystem or Git is used by these unit cases.

Command:

```sh
go test ./internal/daemon ./internal/cli -run 'TestRetirementIssueClosed_Identity|TestEvaluateRetirement_(ConservativeDecisions|UnavailableEvidence|ClosureAndEvidence)|TestWriteRetirementPreview_' -short -race -cover
```

Final output retained in `us001-unit-output.txt`: daemon coverage 3.6%, CLI coverage 0.8% (whole-package denominators with focused tests only).

Files:
- `internal/daemon/retirement_unit_test.go`
- `internal/cli/retirement_preview_unit_test.go`

Criterion mapping:
- AC-001: canonical/shared child and manual path rejection; registered remote-only preview includes exact refs in stable output. Actual durable owner enumeration belongs to real-store tests.
- AC-002: matching closed identity, absent/invalid identity, wrong number/repository, malformed URL, open/unknown/null state, closure reason independence, failed lookup retaining typed cause and blocking Git inspection. Actual HTTP status/cache behavior belongs to client real-response tests.
- AC-003: recent completed run versus old creation/current UpdatedAt, exact 14-day expiry boundary, no-run creation fallback, missing run finish, active and pending work.
- AC-004: Git verification failure, unsafe checkout, dirty files and locally unpreserved commits defer all resources; missing upstream with preserved base is allowed. Actual dirty/ignored/fetch probes belong to Git fixture tests.
- AC-005: independent local/remote ancestry, preserved unmerged history, another checkout using branch, protected branch, absent upstream.
- AC-006: stable advisory CLI output and ownership retained; structured issue state/reason, supplied observation time, retention source/time/expiry, base and exact resource SHA evidence; inventory errors propagate without proposing deletion. Preview seams expose no mutation operations. Durable scheduling nonmutation remains covered by real-store tests.

Initial focused daemon command also passed (3.5%). Combined command passed before the final evidence cases were added, then final command above passed after the additional cases.
