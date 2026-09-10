# Engineering status checks — run 0008 / 304

Date: 2026-09-10. Mode: Implement; persisted workflow: 2 (separated).

This run answered the owner’s status request. It made no implementation fixes,
performed no acceptance/code/architecture/plan review, and published no new
engineering completion event. These are fresh checks of the inherited worktree,
not accomplishments attributed from run 0007.

## Executed checks

| Check | Fresh result |
|---|---|
| `go build ./...` | PASS |
| `make check`: formatting | PASS |
| `make check`: `go vet ./...` | PASS |
| `make check`: pinned golangci-lint v2.13.1 | PASS, 0 issues; nonfatal restricted-cache write warnings |
| `make check`: `go test -race ./...` | FAIL; `internal/turn` failed, aggregate command exited 2 |

The root agent collected the terminal aggregate result. All reported packages
except `internal/turn` passed. Observed turn failures included:

- `TestPlanningConversationThroughOwnerImplementation/claude` and `/codex`
- `TestPlanDiscussionThenCompletion`
- `TestPlanFallbackRejectsPreviousAttemptTransport/claude` and `/codex`
- `TestPlanNewColdAttemptClearsStaleTransportAndReadiness`

The observed diagnostic was `coding agent did not start: submission: configured
socket path is too long`. The turn package failed after 604.320 seconds and
produced a lengthy goroutine dump. Tool output was truncated; this report does
not claim a complete failure count or establish the cause of every failure.
This current failure does not establish why the prior coding agent exited 1.

## Evidence bounds

The current factory bundle marks US-001–US-004 engineering complete. That is
factory-supplied state, not a new completion decision made here. The old event
folder `.litefactory-story-events/302` is absent from this worktree; its absence
does not imply the factory lost its durable evidence. No previous TEST_REPORT
existed in this session. Fresh aggregate results do not establish story-level
coverage or acceptance. The final engineering gate is not green.

## Scoped reproduction

The root agent additionally executed:

```sh
go test -race ./internal/turn -run '^TestPlanDiscussionThenCompletion$' -count=1 -timeout=60s
```

Result: FAIL, exit 1; test duration 2.16 seconds, package duration 2.837 seconds.
The diagnostic again explicitly reported `submission: configured socket path is
too long`. The full local diagnostic was retained at
`/private/tmp/lf207-run304-plan-test.log`; this is temporary diagnostic output,
not a committed artifact or a factory-recorded story evidence event. No source
was changed for this reproduction.

## Run 0009 / 305 — fresh recovery-sketch reproduction

The owner requested a serial recovery sketch. Earlier results above belong to
run 0008 and were not adopted as freshly passing checks. Root re-executed only:

```sh
go test -race ./internal/turn -run '^TestPlanDiscussionThenCompletion$' -count=1 -timeout=60s
```

Result: FAIL, exit 1; test duration 2.69 seconds, package duration 3.375 seconds.
The diagnostic again reports `submission: configured socket path is too long`.
Temporary local output: `/private/tmp/lf207-run305-plan-test.log`. This is a
diagnostic capture, not a story completion event. No production source was
changed; build, lint and full aggregate checks were not rerun during this
bounded sketch. The failure remains unresolved; its existence does not prove
the reason for the original coding-agent process exit.

Root also verified local HEAD and cached origin task-branch ref both still equal
`fd2a74c9a81a6accaeeaa98b44facfc2c3b0ae11`. This confirms cached/local repository
state only, not live forge publication. The factory owns remote verification,
commit/push and saved-checkpoint confirmation.

## Run 0010 / 306 — bounded test-unblocking checkpoint

The owner explicitly limited this Implement run to unblocking tests, then stopping
before continuing stories. Workflow 2 and the supplied frozen scope remain; no
acceptance/review or engineering-complete decision is made for US-005–US-012.

Fresh reproduction by the implementation engineer:
`go test ./internal/turn -run '^TestPlanDiscussionThenCompletion$' -count=1`
failed before runner startup with the socket-path error. Log:
`/private/tmp/lf306-plan-diagnosis.log`.

The underlying fixture mismatch was legacy file-producing Plan stubs implicitly
selecting the new typed contract. The test engineer explicitly bound legacy
content/submission version 1 in `setupOutcomeForge` in
`internal/turn/plan_outcome_test.go`. This is three added lines (including its
explanatory comment). Assertions and production code are unchanged. Existing
actual typed broker fixtures retain version 2 and their short resolved private
temporary home. The production socket-length guard remains intact.

Fresh executed checks after that repair:

| Check | Result |
|---|---|
| `go test -race -cover ./internal/turn -run '^TestPlan' -count=1` | PASS; 77.400s, 29.3% statements |
| `go test -race -cover ./internal/turn -count=1` | PASS; 594.698s, 75.7% statements |
| `gofmt -l internal/turn/plan_outcome_test.go` | PASS; no output |
| Root `go build ./...` | PASS; exit 0, no output |

Temporary diagnostic logs: `/private/tmp/lf207-run306-plan-fixtures.log` and
`/private/tmp/lf207-run306-turn-fixtures.log`. Full turn checks finished close to
the default 600-second test timeout, but did not time out. These are executed
checks of the inherited snapshot plus this fixture repair, not completion or
acceptance evidence for any entire unfinished story. Required root `make check`
result is recorded below when it finishes.

### Aggregate timeout and second bounded correction

Root's first `make check` failed (exit 2): formatting, vet and pinned lint
(0 issues) passed, as did all packages except `internal/turn`, which exhausted
the default package-wide 600-second deadline at 600.812 seconds. Diagnostic log:
`/private/tmp/lf207-run306-make-check.log`. This failed attempt is retained as a
failure; it is not recast as a successful gate.

The implementation engineer inspected the timeout stack: active tests had run
only 0–9 seconds, with ordinary git/SQLite work and parallel tests waiting for
slots. Together with the isolated 594.698-second complete pass, this supports
insufficient suite allowance rather than an established deadlock. The test
engineer changed the Makefile test target to explicit `-timeout=20m` and added a
rationale comment. Race detection, all assertions and operation-level deadlines
remain unchanged. `make -n test` passed and emitted
`go test -race -timeout=20m ./...`. No extra broad check competed with the final
root aggregate rerun. This infrastructure change is the second bounded repair;
production code remains untouched.

### Final aggregate result — PASS

Root reran `make check` with
`GOLANGCI_LINT_CACHE=/private/tmp/lf207-run306-lint` and received exit 0.
Formatting, vet, pinned lint and the complete race-test suite all passed. The
turn package completed in 672.931 seconds, exceeding the former 600-second
package allowance and confirming that the explicit bounded timeout was needed
for this measured aggregate run. Final diagnostic log:
`/private/tmp/lf207-run306-make-check-final.log`.

The root's earlier fresh `go build ./...` also passed; subsequent changes were
only the Makefile test deadline/comment and session reporting, so no further
build or test rerun was necessary. Tests are unblocked at this checkpoint.
Work stops here as the owner requested; US-005–US-012 have not been declared
engineering complete and no acceptance/review was performed. No commits,
pushes, forge mutations, daemon-state changes or mode transitions occurred.


## Run 0011 / 307 — US-005 and US-006 engineering

Selected frozen revisions: US-005 #219 `7967a1c171ba3b3a781bb7df635dd06f53ea33a633212473c5734d67791701be`; US-006 #220 `f856441f38b4aaca0eb4207ffdfd635b8e4545de0af4bfaabe7d874fb8e32fad`. Workflow 2. Inherited implementation preserved.

Initial scoped command: `go test -race -timeout=20m ./internal/planbrief ./internal/turn`. Planbrief passed (1.751s); turn failed (649.258s), exit 1. New regression tests exposed two production defects:

- `TestSubmissionReferenceReworkChecksCurrentBytes`: two inherited submission rejection/recovery SQL inserts supplied seven positional values to the existing eight-column table. Explicit column lists now preserve the eighth column default.
- `TestTypedPlanRunnerPublicationAndDurableReceipt`: new typed Plan feature persisted workflow 0, causing unsupported-contract diagnostics. New feature creation now explicitly persists workflow 2; existing workflows remain unchanged.

Cycle 1 scoped check is running with the same flags, adding the affected store package: `go test -race -timeout=20m ./internal/planbrief ./internal/store ./internal/turn`. No result is inferred until exit.

Cycle 1 result: PASS, exit 0. `internal/planbrief` and `internal/store` passed from cache; `internal/turn` passed in 637.373s. Flags were unchanged. Immediately published `.litefactory-story-events/307/000001.json` (US-005) and `000002.json` (US-006), attempt 1, with empty acceptance/review and the executed result bound to the shared sorted 31-file snapshot digest `a26a82b05d84c2c9382d7422f7ba4c44dee658713c52c0fe460abd158d37b8b1`. Each event is 527,648 bytes.

`go build ./...`: PASS, exit 0. The single final `make check` invocation has passed formatting, vet and pinned golangci-lint v2.13.1 (0 issues), and is executing the full race suite. The linter emitted nonfatal warnings because its default cache directory is outside the sandbox writable roots; no permissions were widened. Final aggregate exit is recorded below when available.

### Final run 0011 result

PASS: the single `make check` invocation exited 0. Formatting and vet passed; pinned golangci-lint v2.13.1 reported 0 issues; `go test -race -timeout=20m ./...` passed. Fresh aggregate results included CLI 244.027s, daemon 386.106s, config 11.321s, git 101.629s, gittest 9.708s, identity 14.283s, report 26.885s and repos 13.591s; all remaining tested packages passed from cache, including the exact scoped turn snapshot. `go build ./...` also exited 0. No flags were changed and full `make check` ran once.

Both event manifests, base64 contents, SHA-256 values and sorted file lists were checked against current worktree bytes after publication and still match. No source changes followed scoped execution. The shared digest is `a26a82b05d84c2c9382d7422f7ba4c44dee658713c52c0fe460abd158d37b8b1`. The event transport is retained for the factory and is not committed or deleted by agents.

US-005 and US-006 selected engineering work and aggregate checks are complete. Acceptance and reviews remain untouched; factory save/commit/push and remote verification remain pending outside this run. US-007–US-012 were not selected.


## Run 0012 / 308 — US-007 checkpoint

Engineering only, persisted workflow 2. Frozen US-007 #221 revision `8933dc2eb8cbb078ed8a669705f023d8b33344be85a9862a5674d13a3f46ce94`.

- Pure parser/graph regressions: 25 named scenarios covering all five sections, ID retirement/nonreuse/remapping, multiline/reorder equivalence, ambiguity and owner text preservation.
- `go test -race -timeout=20m ./internal/planbrief`: PASS, 1.735s.
- `go test -race -timeout=20m ./internal/store`: PASS, 109.557s; offline real SQLite history/restart checks.
- `go test -race -timeout=20m ./internal/turn -run TestStoryReconciliation`: PASS, 21.437s.
- `go test -race -timeout=20m ./internal/turn`: PASS, 648.448s after all US-007 files finalized.
- Pinned affected-package golangci-lint v2.13.1: PASS, 0 issues. Default lint cache access was denied; writable `/private/tmp/lf-207-golangci-cache` used without permission widening. Initial lint issues and an overly specific test diagnostic assertion were corrected before the passing checks.
- Atomic event `.litefactory-story-events/308/000001.json`: attempt 1, 10 files, digest `f3c51948f891329e72ef7e6756a79b21da3eaccd4fe05386d28c4bd8954bc69d`; empty acceptance/review.

US-008/009 and final aggregate checks remain in progress. No live-provider behavior is inferred from synthetic fixtures.


### Run 0012 — US-007 replacement and US-008 checkpoint

US-008 required modifying retained `internal/turn/typed_plan.go` and `internal/turn/plan_publication.go` so completed legacy children remain opaque unchanged dependencies. Published US-007 invalidated event 308/000002.json before these writes. No historical acceptance changed.

- Added 20 pure conversion/preservation/CLI argument scenarios and 5 offline CLI boundary cases. Store/turn fixtures cover exact preview/apply, unsafe/stale refusal, durable locks, partial publication, restart/lost reply, confirmation conflict, legacy history, completed dependency preservation and persisted generation dispatch.
- Full `go test -race -timeout=20m ./internal/planbrief`: PASS, 1.720s.
- Full `go test -race -timeout=20m ./internal/store ./internal/turn ./internal/cli`: PASS, exit 0; store 158.441s, turn 706.164s, CLI 235.952s.
- Final lint caught two test-only HTTP status literals; replaced with standard `http` constants. Full final-snapshot `go test -race -timeout=20m ./internal/cli`: PASS, 220.646s. Final pinned affected-package lint: PASS, 0 issues. Test fixture API/config corrections occurred before passing focused fixtures; no production defects were exposed by this test stage.
- Immediate replacement US-007 attempt 2 event 308/000003.json and US-008 attempt 1 event 308/000004.json share final 22-file digest `8e0cec441046f0e2e7b4518e4f9a4376f7346a3018fc6a6d6645446483a952c8`. Acceptance/review remain empty.

US-009 and final aggregate checks remain in progress.


### Run 0012 — US-009 checkpoint

- Full `go test -race -timeout=20m ./internal/teamsource`: PASS, 13.570s.
- Documented `-run TestSharedWorkflow` procedure: PASS (cached), exercising 24 in-memory role renders across two harnesses and four modes.
- Executed temporary Python assertions: 12 source mappings, 24 installed role targets, 6 additional targets exist; all 43 installed instruction files have unchanged before/after SHA256; literal `- Mode: X` instruction preserved.
- `git diff --check`: PASS from implementing specialist.
- Immediate US-009 attempt1 event 308/000005.json: 14-file digest `6daccdf6d077d9e30c0406eaff326b74fbd128d976857265edefbbcf86315cb0`; empty acceptance/review.

These checks verify shared source rendering and manifest consistency, not installed wrapper byte parity or authentic agent capability. One final aggregate `make check` and `go build ./...` are now running.


### Run 0012 final aggregate — PASS

- `go build ./...`: PASS, exit 0. No `dist/` write or daemon operation.
- Exactly one final `GOLANGCI_LINT_CACHE=/private/tmp/lf-207-golangci-cache make check`: PASS, exit 0. Formatting, `go vet ./...`, pinned golangci-lint v2.13.1 (0 issues), and `go test -race -timeout=20m ./...` all passed. Flags were not changed during the run.
- Aggregate package results included CLI 261.170s, daemon 386.604s, git 101.585s; planbrief/store/turn/teamsource and other unchanged test inputs reused valid passing Go cache entries. Cached results are recorded as cached, not represented as newly executed uncached tests.
- Final current event integrity check passed: events000003–000005, 58 retained file references, exact worktree bytes/content hashes/manifest hashes; empty acceptance/review and matching per-check implementation digests. No implementation/source edits after this check.

Selected US-007, US-008 and US-009 engineering and executed checks are complete. Factory save/commit/push remains external to this run. No acceptance/review or automatic Validate was performed. Remaining US-010–012 were not selected; installed instruction parity and authentic two-harness observations remain pending their specified work.


## Run 0013 / 309 — US-010 scoped checks

US-010 (#224), frozen revision `5fa9fb932cb515b31696d71b59e2efdc3de5d93f310dbc9401312221058198b4`, engineering event `309/000003.json`, attempt 1. Snapshot: 16 exclusively `.claude/**` files, digest `b1945927ccba057b85a2b5ba8d93f3b73012891cd2139e5d2d691ed26cd21e95`. Acceptance/review arrays are empty.

- `python3 /tmp/lf309-story10-checks.py`: PASS, seven scoped consistency assertion groups. Script retained below and in immutable event check evidence.
- `go test -race -timeout=20m ./internal/teamsource -run TestSharedWorkflow`: PASS (cached), offline rendering only.
- `git diff --check -- .claude`: PASS.
- `go build ./...`: PASS, exit 0.
- One final `make check`: running; format/vet/pinned lint have passed, with nonfatal lint-cache write warnings from sandbox permissions. Test flags remain unchanged.

US-011 requires manual Claude execution of the Codex targets in `docs/story-contract-instruction-manifest.md`. US-012 dependency and aggregate parity remain blocked. Authentic external helper/correction/decision captures for BOTH actual harnesses are not supplied. These checks establish no actual runtime capability and no acceptance.

### Reproducible scoped assertion script

```python
from pathlib import Path
import re, subprocess, hashlib
root = Path.cwd()
manifest = Path('docs/story-contract-instruction-manifest.md').read_text()
targets = sorted(set(re.findall(r'(?<![\w/])\.claude/(?:agents|modes|skills)/[\w/.-]+\.md', manifest)))
assert len(targets) == 16, targets
changed = subprocess.check_output(['git','diff','--name-only','--','.claude'],text=True).splitlines()
assert sorted(changed) == targets, (changed, targets)
print('PASS manifest: exactly 16 scoped Claude targets changed')
source = Path('teams/source/skills/workflow/SKILL.md').read_text()
installed = Path('.claude/skills/shared/workflow.md').read_text()
section = '## Structured content and submission contract\n'
assert installed.split(section,1)[1] == source.split(section,1)[1]
print('PASS structured shared workflow: byte-identical installed contract')
roles = [p for p in targets if '/agents/' in p]
for path in roles:
    value = Path(path).read_text()
    baseline = subprocess.check_output(['git','show','HEAD:'+path],text=True)
    assert value.split('---',2)[1] == baseline.split('---',2)[1], path
    for text in ['.claude/skills/shared/workflow.md','preflight/submit/receipt','list-B evidence','explicit legacy upgrade','does not widen','Pending\nreceipts and synthetic fixtures never establish authentic runtime delivery']:
        assert text in value, (path,text)
print('PASS 12 roles: original tool/model wrappers and shared protocol handoff preserved')
for path in targets:
    value = Path(path).read_text()
    baseline = subprocess.check_output(['git','show','HEAD:'+path],text=True)
    assert re.findall(r'^.*- Mode:.*$', value,re.M) == re.findall(r'^.*- Mode:.*$',baseline,re.M),path
print('PASS literal mode-header lines preserved for all 16 targets')
for path in ['.claude/agents/'+role+'.md' for role in ['plan-orchestrator','requirements-engineer','go-architect','proxy-product-owner']] + ['.claude/modes/session.md']:
    value = Path(path).read_text()
    for text in ['user_story:','acceptance_criteria:','tasks:','architecture:','dependencies:','factory renders','Null, duplicate,','link-only Story','contract 1','contract 2']:
        assert text in value, (path,text)
print('PASS 4 Plan roles and session: five typed sections, strictness, rendering and legacy dispatch')
for text in ['sequence and attempt. Never supply those envelope fields','never put contribution JSON in argv','Pending, failed, missing or disconnected\nis never success','exit zero and a claimed summary prove none','without executing new ones','uses list B exclusively: 1–16 objects','8,192 UTF-8 bytes','story-contract upgrade <parent-ref>','next approved scope','Never automatically chain or dispatch modes','Never widen permissions, launch paid nested']:
    assert text in installed,text
print('PASS shared contract assertions: authority, bounded helper, honest receipts, evidence, editing, upgrade and lifecycle safeguards')
for path in ['.claude/skills/shared/review-evidence.md','.claude/modes/validate.md']:
    value = Path(path).read_text()
    assert 'read-only' in value and 'list-B' in value and 'retained' in value,path
print('PASS Validate targets: read-only retained list-B guidance')
print('FILE MANIFEST')
for path in targets:
    print(hashlib.sha256(Path(path).read_bytes()).hexdigest(),path)
print('Scoped checks do not read or require .codex parity; US-011 remains a manual Claude handoff.')

```


Run 0013 final: `go build ./...` and the single final `make check` passed, exit 0. Format, vet, pinned golangci-lint v2.13.1 (zero issues) and full race suite passed; turn 730.400s, daemon 391.349s, CLI 259.218s. Linter emitted nonfatal cache-write permission warnings; no permission changes or check retries occurred. Published US-010 event 309/000003.json was verified against all 16 final worktree files. No `.codex` diff exists.

Selected scope remains partial: US-010 engineering complete; US-011 blocked for manual Claude execution of the exact Codex targets in docs/story-contract-instruction-manifest.md; US-012 blocked on US-011 and cannot claim aggregate installed-tree parity. Authentic external evidence for both actual harnesses remains missing/unverified without a waiver. Existing US-001–009 work was preserved. Session artifacts record progress outside the exclusively `.claude/**` US-010 implementation snapshot. No acceptance/review, issue closure, forge actions, commit/push or automatic mode transition. Factory saving remains pending.
