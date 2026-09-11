# Implementation notes — issue #210 run 0005

Selected mode is Implement, persisted workflow version 2. Frozen US-001 (#237), US-002 (#238), and US-003 (#239) are the sole story requirements and execute in dependency order. Acceptance and code/architecture/plan review remain for separately requested Validate; the factory owns save and forge effects.

## Decisions and boundaries

- Reuse the existing session across dates. No questions are needed or permitted in this run.
- Use existing fake runners/clocks/HTTP and migrated temporary SQLite, without integration tags, live infrastructure, nested coding-agent processes or runtime-state edits.
- Retain real scoped and aggregate check output. Engineering receipt input validity, engineering recording and remote effects are reported separately.
- Retry defaults remain the approved poll-based exponential delay, existing one-hour cap and escalation on the third failed eligible recovery.
- Historical incident reconstruction, instruction-tree parity maintenance, forced resend, duplicate cleanup and new configuration remain outside this feature.

## Progress

- US-001 implementation dispatched with its frozen identity, revision, dependencies and complete story obligations inline.
- US-001 focused engineering checks passed: pure daemon repair, migrated SQLite daemon restart/selection, store/turn fault boundaries, recovered resume token, and readiness-refusal report. Real SQLite failure triggers cover final session-running, resume-token and terminal-result writes without production test hooks.
- US-001 capture `us001-run0005-v1`, digest `9a1086b4c881b5895e7bb8953ee1552ad830238a69803202e0d03cdbf0e58609`, contains 28 changed implementation/test files and five executed check records. Helper preflight valid/complete, no diagnostics; receipt `c7c69de64caa799138d0c3c73efa9f1e`: InputState valid, EngineeringState recorded, AcceptanceState not_applicable, EffectState pending. No acceptance or save is claimed.
- US-002 implementation started after recorded dependency engineering, split between store delivery persistence and turn Reporter orchestration. A pending refusal explanation must recover before readiness is reevaluated for the original trigger.
- Earlier interrupted and failed test attempts remain retained separately. Only actual passing scoped checks were submitted. Repository-wide `go build ./...` passed at the US-001 snapshot; full store/turn suite and final aggregate checks remain pending.
- US-002 engineering recorded after passing pure identity/matching and SQLite/fake-HTTP fault/reconstruction suites, including refusal replay after readiness changed, retained bytes with a failing replacement renderer, equivalent duplicate selection, and redirected child source accounting. Capture `us002-run0005-v1`, digest `d37e32361f714199d22dba9cc2b1413cfe4b4d580406b0e33e7c0d513cb5574e`, 37 changed files and three checks; receipt `8f17d03c873d9a48e58d10e1eec3b271`: valid input, recorded engineering, acceptance not applicable, effects pending. Repository `go vet ./...` passed at this stage.
- Generic render failures now propagate typed delivery errors; bounded loud logging is daemon-owned in US-003 instead of repeated Reporter-level logging. Tests assert retained cause and zero external/acknowledgment effects.
- US-003 implementation started with separate store metadata and daemon scheduling/diagnostic ownership. Pending lookup during total storage outage may require source-keyed in-process scheduling until the persisted delivery identity is available; it must not invent a persisted delivery key.
- US-003 scoped pure clock/diagnostic and SQLite/restart/Tick/ordering tests passed. Initial aggregate lint found an obsolete unused source-consumption helper; removal fixed it. The linter uses a writable temporary cache, without changing permissions or runtime configuration.
- The subsequent aggregate exposed actual ancillary bookkeeping gaps: trusted completed cancellation receipts were ignored, and pending report recovery did not retire mode journals immediately. The second fix cycle corrects those using existing routines and retains/resolves cleanup debt. Related tests preserve owner labels, exact command ordering and no reruns. Other old fixtures were updated to require trusted comment evidence and eligibility instead of blind reposting, and to allow the new authorized delivery marker while retaining injection checks.
- Focused second-cycle daemon and interruption replay checks passed; outputs are in checks/aggregate-cycle2-*.log. Final aggregate make check is rerunning; no aggregate success is claimed yet. Earlier aggregate attempts remain checks/aggregate-make-check-attempt1.log and checks/aggregate-make-check-attempt2.log.
- Final aggregate make check and safe build passed after the second fix cycle. All three final immutable engineering captures were submitted and receipt-queried successfully; details and exact request IDs are in TEST_REPORT.md. The helper demanded explicit engineering invalidation before refreshing earlier unaccepted US-001/US-002 snapshots; those events preserve prior evidence, and subsequent final captures are recorded. All final effects remain pending and acceptance is not applicable in this mode.
- Ordinary ambiguity resolved within the approved scope: startup failures with no tick timestamp schedule their first eligible recovery from the first tick plus poll interval. Resolved diagnostic metadata is retained but inactive. No new settings, retry bypass, external framework or additional owner decision was introduced.


## Run 0006 — aggregate recovery (2026-09-11)

The header and explicit recovery contract select Implement despite the owner's acceptance wording. Ran aggregate checks only; preserved US-001/002/003 as completed engineering dependencies. `make check` failed in the teamsource research-orchestrator instruction-handoff test; `go build ./...` passed. See current TEST_REPORT.md and run0006 logs. No source/test fixes, story events, acceptance review, runtime-state edits, commits or pushes. Save is not confirmed.
