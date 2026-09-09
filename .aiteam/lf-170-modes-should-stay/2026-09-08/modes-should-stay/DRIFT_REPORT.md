# Drift Report — modes-should-stay, run 0013

## Verified this run

Generation-2 US-001, US-002 and US-003 passed fresh acceptance checks, independent scoped review and race tests. Their immutable evidence events were written immediately in dependency order. US-004 runtime criteria AC-004-1, 2, 3 and 6 passed fresh checks. Earlier run claims were treated as unverified.

## Changes

Added one test file, internal/daemon/mode_retirement_internal_test.go, covering actual classifier-free retirement with migrated SQLite. No production changes or new requirements.

## Remaining gap

US-004 AC-004-4 and AC-004-5 remain blocked: three Codex skill copies still lack matching Plan discussion/continuation instructions, and .codex is read-only in this run. The existing CODEX_SKILLS.patch passes a fresh applicability check. No acceptance criterion was dropped or weakened.

## Aggregate gates and verdict

go build ./..., make check and full go test -race -cover ./... passed. Aggregate review is BLOCKING only for the protected source/copy parity gap. APP context and constraints remain semantically accurate without updates. All published verified snapshots still match.

DRIFTED — implementation completion remains blocked on the two US-004 documentation criteria. Apply the prepared patch in an authorized writable environment, then reverify US-004. Factory event submission is confirmed; issue closure is not asserted by this report.
