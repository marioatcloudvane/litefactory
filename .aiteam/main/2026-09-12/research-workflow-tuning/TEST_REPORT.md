# Verification

- PASS: `go build ./...` (nonfatal cache-write warning).
- PASS after final fixes: `gofmt -l .`, `go vet ./...`, pinned `make lint` (0 issues).
- PASS: engineer's focused source rendering, roster, snapshot, remote/parity and team checks.
- PASS after fixture correction: `go test -race -timeout=8m ./internal/config ./internal/cli`.
- PASS on focused race rerun: `TestRunTimeoutKillsTheWholeSubtree`.
- FAIL: required `make check`. Format, vet and lint passed; the race suite exposed an introduced CLI fixture mismatch (fixed and full package rerun passed), a transient harness fixture failure (rerun passed), and existing teamsource exact-prose parity failures. All other package results passed.

Remaining failures: `TestBothInstructionTrees_StructuredProtocolHandoffPointsAtOwnTreeOnly`, `TestCodexSkills_SharedSourceParity`, `TestCodexSkills_StructuredProtocolHandoffScopedToCodexTree`, `TestCodexSkills_AC003GuardClausesPreserved`. Engineer confirmed historical assertion mismatch predates this work; no unrelated instruction contracts rewritten.

Filesystem approvals were required for protected Codex edits, Go caches and test HTTP listeners. No daemon runtime changes or release publication performed.
