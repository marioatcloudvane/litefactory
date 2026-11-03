AI;DR: Factory action validation and draft ownership are repaired, and affected checks pass. Independent acceptance and the full repository check remain with Validate.

# Factory action ownership repair

Engineering evidence for the supplied architecture finding, based on starting commit `c180ed686fd2cccdcd0b6f80483c20a8e674394c`.

## Change and compatibility

- Factory owner-action code now owns fixed changing-action schemas, scalar validation, input budgets, installed-capability availability and action discovery. Resolve and recheck no longer accept a validation callback into messaging.
- Factory feature drafting owns project selection, replay, preview/creation-lease fences, isolated response validation and retry, protected content, confirmation identity/hash/code, expiry and persistence. The remaining messaging adapter supplies primitive content/model/history capabilities and presentation conversion.
- Remote composition constructs core domains directly, without creating a messaging service for action decisions. Existing host authorization, effect journals and approval channels remain in place.
- Preview fields remain unchanged through factory-owned contract aliases. Invocation fields keep their prior names and saved encodings. No schema migration, authorization-generation change or pending-action conversion was introduced.
- Three subagents worked in parallel on validation/catalogue extraction, draft extraction and consumer/host wiring. The root worker reviewed all changes, added direct factory regression coverage and ran all verification.
- No scope deviation or changed approval rule was required.
- A second read-only subagent audit found no concrete remaining defect in the reported boundary. It compared draft operation ordering, statuses, markers, hashes, body and history serialization with the starting code, and found the moved rules retained. This source audit is engineering review, not independent acceptance.

## Regression evidence

`TestFixedActionValidationAndAvailability` checks installed capabilities, caller-map isolation and that a consumer schema cannot weaken factory validation. `TestActionFieldDiagnosticsKeepLimitsAndPrivateValues` checks retained budgets and private diagnostics.

`TestFactoryDraftRetainsPreviewAndCreatingLease` invokes factory drafting directly with a temporary database and controlled generation. It checks retained identity/hash/code/expiry, marker removal, same-message replay without generation, exact expiry refusal and both independent creation fences. Refused replacement leaves the pending create unchanged.

Existing draft, instruction-capture, pending diagnosis/standards payload and remote-without-Discord tests remain in place. The existing parameterized GitHub/Linear main path remains unchanged.

## Verification

All commands use local fixtures; no production service was tested. Complete package runs use finite timeouts, and the root alone ran tests.

- Passed: focused factory/draft and affected action/standards/invocation tests.
- Passed: `go build ./...`.
- Passed: `make fmt` and documentation link resolution.
- Passed: `make vet` (repository-wide).
- Passed: `GOLANGCI_LINT_CACHE=/tmp/lf1362-repair-golangci-cache make lint` using pinned golangci-lint v2.13.1; zero issues after simplifying one redundant selector in the new daemon wiring.
- Passed: `go test -count=1 -timeout=30m ./internal/cli ./internal/store`.
- Passed: the Makefile's 82 CLI and 136 store race selections, each using `go test -race -count=1 -timeout=30m -run '^(<Makefile selections>)$'`.
- Passed: `go test -race -count=1 -timeout=30m ./internal/factory/... ./internal/messaging/... ./internal/messagingstate ./internal/daemon ./internal/connector ./internal/owneraction`. The daemon package completed in 1293.002 seconds; its full run includes the GitHub/Linear main path.
- Passed: final `go test -race -count=1 -timeout=30m ./internal/daemon` after the redundant-selector lint correction; 1232.999 seconds. Other package source has not changed since its complete passing run.
- No pre-existing test failures were observed in the executed scope.

Completed output is retained under `logs/`; `race-commands.txt` contains the exact executed Makefile race selection commands. `changed-files.sha256` identifies changed source and documentation files only; it is not an aggregate snapshot capture.

The initial focused compile identified the not-yet-added draft readiness method while parallel edits were being integrated. It was added, and the focused run passed. The first lint run reported one redundant embedded-field selector; that was corrected and the full lint rerun passed. These are resolved repair findings, not pre-existing failures.

## Limits and handoff

The worker role and go-stack explicitly prohibit `make check` and the whole test suite in this stage. Those were not run; the full repository aggregate remains for Validate, despite the owner's request for it. Individual repository formatting, vet and lint checks were run as recorded above.

Controlled generation/providers and offline compatibility tests do not prove live delivery or live process stop/drain and previous-code recovery. No new live recovery rehearsal was performed. Engineering completion does not establish acceptance or publication. Owner check pending: AC-001, AC-002, AC-003, AC-005, AC-006, AC-007.

The supplied reviewer recorded existing agent behavioral guidance in Go strings at `internal/harness/prompt.go:450`, `457`, `463`, `468` and `514`; the frozen architecture also records `758` and `770`. That file is unchanged; this existing guidance issue remains for operator disposition. Existing command descriptions were moved unchanged with their schemas, without adding agent behavior.
