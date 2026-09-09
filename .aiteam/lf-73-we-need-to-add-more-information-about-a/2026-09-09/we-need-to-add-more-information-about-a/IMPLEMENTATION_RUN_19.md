# Implementation run 19

Run 266 continues the selected US-1, US-2, US-3 and US-7 revisions. US-4, US-5 and US-6 are completed dependencies and are not being reimplemented.

The owner explicitly requested completion of the remaining implementation while deferring tests. This run therefore performs production implementation, formatting, build, static analysis and scoped code review without executing test suites or paid provider probes. Missing real-execution fixtures do not stop implementation; they remain unverified evidence and cannot be reported as passing acceptance checks.

## Work coordination

- The Claude engineer owns production changes in `internal/harness`.
- The Codex engineer owns production changes in `internal/codex`.
- Shared contract changes require coordination before either engineer edits outside those packages.
- The root reviewer reviews each stable provider change promptly, before waiting for the other provider.
- Factory event transport belongs exclusively to this orchestrator. A story cannot receive a `verified` event without the required genuine passing acceptance, review and test evidence.

## Status

US-1 has no remaining production-contract change identified by static review. US-2 and US-3 production collection is implemented and passed scoped static review after fixes. US-7 operator source documentation and context updates are complete; its real-execution and test evidence remains deferred.

Claude now forwards descendant messages and supplements the stream with safe associated transcript reads. Prelaunch message baselines prevent resumed history from becoming new usage; message models and Agent/Task identities preserve attribution. Finalization waits within a two-second bound for both child completion and usage, retaining partial evidence on cancellation or missing output.

Codex now reads associated rollout session ancestry, matching turn models and exact per-response usage records. Prelaunch boundaries and historical response digests suppress replay, copied parent-thread records are excluded, and lifetime summaries remain non-additive. Discovery and finalization are bounded, with descriptor checks and no runtime-state writes.

## Validation

- `go build ./...`: passed on the final production snapshot.
- `go vet ./...`: passed on the final production snapshot.
- `gofmt -l` on all six changed production files: no output.
- Root scoped and aggregate static production review: PASS, recorded with source hashes in `CODE_REVIEW_RUN19.md`.
- No tests, `make check`, or paid provider probes were executed. No passing test or real-execution qualification is claimed.

An earlier aggregate build overlapped the Claude lifecycle edit and failed on a transient missing struct field. After the owner completed that edit, the final aggregate build and vet both passed.

## Factory status

This run publishes only blocked verification events for selected US-1, US-2, US-3 and US-7. Their reasons distinguish completed implementation/static review from deliberately deferred acceptance tests and authentic provider captures. The event schema requires passing tests and every acceptance criterion for a verified event; build and static review are not substituted for those results. Completed US-4, US-5 and US-6 receive no new outcome events.

## Operator handoff

`docs/meter.md` links to `docs/meter-provider-sources.md`, which records implemented collection, source provenance, limits and qualification status. `.aiteam/APP_CONTEXT.md` reflects the new collectors. Existing constraints remain unchanged. The factory owns committing and publishing these changes.
