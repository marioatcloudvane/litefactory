# Test report — run 0017

## Final aggregate result

PASS: `GOFLAGS=-p=1 GOLANGCI_LINT_CACHE=/tmp/lf73-run261-golangci-cache make check` exited 0. Formatting, go vet, pinned golangci-lint v2.13.1 (0 issues), and full `go test -race ./...` passed. Serial package scheduling preserves all tests.

Selected full race package durations: cli156.893s, codex38.549s, daemon145.627s, git112.991s, harness15.601s, meter1.528s, report3.107s, store66.233s, turn326.808s. Remaining packages also passed or had no tests.

PASS: `GOFLAGS=-p=1 go build ./...` exited 0. No dist output was built.

## Scoped checks and independent review

Independent integrated scoped race passed for store/turn/meter/cli/report with TestMeter, TestPrice and the no-ambient-turn-deadline invariant. Duration store265.760s, turn37.971s, meter1.770s, cli211.001s, report2.495s. Provider package race suites and focused pricing/report/CLI checks also passed.

Production synthetic transport test `TestMeterSyntheticTransportPersistsAndReports` passed: real shell fixture runner → production runMetered usage sink → durable measurements/valuations → prepared report. Claude duplicate message usage is counted once; Codex inclusive summary remains nonadditive and unknown. These are synthetic local integration checks, not authentic provider certification.

Independent reviews: CODE_REVIEW_RUN17_SCOPED.md and CODE_REVIEW_RUN17_AGGREGATE.md. Identified diagnostics/provenance/identity/pricing-validation/CLI-output issues were corrected and reviewed again.

## Lint correction and evidence lifecycle

First aggregate attempt stopped on one test-only predeclared identifier in internal/report/meter_test.go. US4/5/6 evidence was invalidated BEFORE renaming max to maximumTokens. Focused report test passed0.528s, independent mechanical review confirmed unchanged behavior, and fresh verified events were published. Final full make check then passed.

Final verified events000013/000014/000015 carry US4/5/6 respectively, each with52 captured implementation/validation files and manifest SHA256 `28b760b6687af8df7c3a5d98c8bafbe1a292ed68471bd6a3bf8f0c201b8efa9f`. Root independently checked file bytes, content hashes, manifest and criterion counts7/6/10. No captured files changed after publication.

## Deferred validation

Owner explicitly deferred real provider captures. US1 AC1.4 and full US2/US3/US7 provider descendant/model certification are not passed. Stream collection remains conservative with functional gaps described in DRIFT_REPORT.md and docs/meter.md. No paid provider process or integration build tag was added to committed tests. Automated tests do not establish complete real-provider support.
