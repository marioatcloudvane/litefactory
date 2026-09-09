# Implement notes

## Run 0015 current status

Consumed the revised factory-selected US-1 revision `5f9601901d81707a291188df53dbab44dc139f6d81e4d92c51e7659331a0e794`. Its AC-1.4 accepts externally captured real executions committed as sanitized fixtures with provenance; nested provider execution inside this run is not required. Earlier environment failures below are historical, not reverified findings of run 0015.

Delivered partial US-1: optional neutral usage capture at the existing agent boundary, defensive copying of all token/event/coverage data, checked nullable counter normalization and arithmetic, explicit counter/scope semantics, deterministic replay identities and pure consumer-owned checkpoint transitions. No actual provider is declared supported, and neither adapter emits new usage yet. Independent acceptance tests exercise ownership, cancellation, invalid/subset/overflow counters and baseline isolation across attempt/source/child/model/counter/epoch.

The implementation engineer inventoried `internal` and `docs` paths for testdata/capture/provenance. Existing `internal/harness/testdata/quota-authentic.jsonl` and its `QUOTA_PROVENANCE.md` contain selected Claude Code 2.1.263 quota diagnostics, not full conversation evidence. The corresponding Codex quota fixture omits conversation handles and has no known historical executable version. Neither establishes complete hierarchy/model attribution. New arithmetic and capability tests are explicitly synthetic and cannot satisfy AC-1.4.

US-1 remains blocked solely at the outstanding provider evidence obligation after partial code validation; US-2 through US-7 remain unstarted dependencies. No migration, pricing, CLI, CSV or collector integration was substituted. To resume, supply sanitized external root/parallel/nested captures with exact agent versions/source formats and recorded provenance; subsequent adapter stories also require failed descendants, model changes and replay/resume scenarios. No in-run nested launch is a prerequisite.

The scoped reviewer found the partial code CLEAN with no advisories and independently passed race tests. Separate unit-test engineering added six test functions with 18 table cases: `go test -race -cover ./internal/agent ./internal/meter` passed (agent 81.6%, meter 89.9%). Safe `go build ./...` passed. The first aggregate check found three missing exported constant-block comments; these were corrected with no functional change, and scoped tests passed again. The aggregate retry uses a writable temporary lint cache and `GOFLAGS=-p=1` to avoid the previously documented package-contention baseline timeouts; no test is excluded and the race flag remains enabled. Final aggregate result is recorded in the run entry below.

`APP_CONTEXT.md` now describes the partial contract and pure meter helpers explicitly, including missing adapter wiring and real provider fixtures. Existing constraints already cover the work and were not rewritten. No forge access, commits/pushes, runtime-state changes, paid-agent test launches, historical importer or dist writes.

## Run 0015 final validation and events

- `go build ./...`: passed, exit 0.
- `GOLANGCI_LINT_CACHE=/tmp/lf73-run253-golangci-cache GOFLAGS=-p=1 make check`: passed, exit 0. Format, vet, pinned golangci-lint v2.13.1 (0 issues) and the full `go test -race ./...` suite passed. Package concurrency was serialized; no tests were removed or disabled. Daemon passed in 127.436s, store in 34.890s and turn in 294.227s.
- Scoped partial-code review: CLEAN; independent race tests passed. The only aggregate correction was documentation on three exported constant blocks, followed by the passing full gate.
- Root orchestrator published seven atomic `blocked` events in `.litefactory-story-events/253/000001.json` through `000007.json`, retaining supplied issue identities and selected revisions. No `verified` event was published because AC-1.4 remains unsatisfied. The factory owns these transient events and their publication lifecycle.
- Partial US-1 code stays in the worktree for factory persistence. Implement remains unfinished. The blocked fixture requirement is independent of the now-green software checks.

## Historical run 0012 notes

## Severe environment gate

US-1 AC-1.4 is blocked. Installed and authenticated codex-cli 0.153.4 fails before model output when initializing its in-process app-server client: Operation not permitted (os error 1). Two bounded read-only scratch probes, including one without ephemeral mode, produced the same failure. See CAPABILITY_REPORT.md for sanitized commands and observations. Claude Code 2.1.266 yielded some real child/model evidence, but this does not establish the complete hierarchy and model capability contract.

The approved plan selects exact provider surfaces only after this proof and places US-2/3/4 after US-1, with US-5/6/7 downstream. No production schema, collector, CLI or test implementation was started against an unproven contract. Scope remains unchanged; full subagent support was not reduced to root-only support.

## Managed evidence

Published seven atomic blocked events in .litefactory-story-events/245, one for each selected frozen story, sequences 000001 through 000007. No verified event or completed acceptance claim was emitted. Factory owns publication and transport lifecycle.

## Resumption requirement

Provide a managed execution environment where normal authenticated Codex CLI startup succeeds under its standard permission checks, or provide factory-approved version-pinned sanitized real-execution evidence covering all required hierarchy/model scenarios. Then repeat the capability gate and continue the existing dependency order. No permission bypass or credential change is proposed.

## Validation

Safe go build ./... passed. Baseline make check terminal result is recorded below and in MANIFEST.md. Pinned lint reported zero issues but warned that its default cache directory is not writable in this sandbox. These baseline checks are not evidence that the requested feature exists.

## Final baseline validation result

- go build ./...: passed, exit 0.
- make check: failed, exit 2. Format and go vet passed; pinned golangci-lint v2.13.1 reported 0 issues (default cache-write permission warnings).
- go test -race ./...: internal/daemon and internal/turn exceeded the default 10-minute package timeout, failing after 605.286s and 606.989s respectively. Daemon was running TestTheFirstEverTickDoesNotReachBack during fixture migration; turn had multiple ApplyConfirmedResolution and other tests active. This output does not establish one test as the root cause. Other reported packages passed, including cli, codex, harness and store.
- No production or test source changed; these are baseline results, not feature acceptance. No repeated full suite or unrelated repair was attempted after the required provider capability gate was found blocked.
- Scoped review: CODE_REVIEW.md is BLOCKING (CR-B-001, missing mandatory provider capability evidence). No acceptance completion was claimed.
