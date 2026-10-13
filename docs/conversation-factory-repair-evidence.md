# Story 002 repair investigation

The continuation inspected HEAD
`e27c255d`, following the review of `f6c495db`. Three subagents independently
examined scratch canonicalization, available retained evidence, and story
ownership. No production files were changed.

The reported graph-delivery failure is outside the story's implementation:
`internal/turn/bug_graph_runtime_test.go`, `internal/turn/sync_base.go`, and
`internal/git/clone.go` are unchanged between the reviewed commit and HEAD.
Story 002 changed messaging, store, and daemon conversation integration.

The graph fixture derives its factory home from the canonical temporary
repository root. Scratch creation uses that home's `implement-scratch`
directory. Git correctly rejects permission-denied ancestor resolution before
checking clone overlap; treating that denial as a missing directory would
weaken isolation.

The sandbox source supplies a plausible environment explanation:
`internal/turn/validate_runtime.go` places the suite checkout beneath factory
home's `validate-scratch`; `internal/daemon/setup.go` denies reads of factory
home; `internal/bugchecks/executor.go` grants writes to the checkout and temporary
roots while retaining those denies. This can explain an authorized descendant
with a denied ancestor. The current bundle, session descriptor directory, and
exposed factory APIs did not supply the retained `suite-20-0` log or generated
runtime profile. Consequently this investigation cannot identify the exact
runtime denial or claim the Validate environment was repaired.

The named failing test was rerun in this worker's authorized environment:

```text
go test ./internal/turn -run '^TestGraphBugDeliveryConcreteConflictAndOwnerWait$' -count=1 -timeout=10m
ok github.com/cloudvane/litefactory/internal/turn 49.478s
```

All eight variants, including budget and mixed conflicts, passed. This focused
diagnostic run is not a complete package run or a replacement for the failed
retained suite. The worker role prohibits running the whole repository suite
and changing unrelated stories. A repair of the separate Validate sandbox needs
its retained log/profile and authorized ownership of that environment; the
production permission-denial boundary remains intact.

The complete story conversation package was also checked on this HEAD:

```text
go test -race ./internal/messaging -count=1 -timeout=0
ok github.com/cloudvane/litefactory/internal/messaging 330.728s
GOLANGCI_LINT_CACHE=/tmp/lf777-repair-lint-cache make lint PKGS='./internal/messaging'
golangci-lint v2.13.1 (installed)
0 issues.
```

The complete messaging run includes the main-path journey through both Claude
and Codex fixtures. `gofmt -l internal/messaging/conversation*.go` returned no
findings, and `git diff --check` passed. These fresh local results establish
engineering checks only. They do not replace the retained full-suite check,
claim manual acceptance, or demonstrate live provider or external effects.

## Following worker continuation

The next continuation independently reviewed authorization, exact confirmation,
bounded coordination, retained sources, fencing, and saved-outcome recovery with
three subagents. It found and fixed one story-owned follow-up gap: meter lookups
now retain the task argument in factory-issued target references. The regression
reads a task, reloads its saved reference, follows that reference, and verifies
that revoked channel access prevents another read. No scratch canonicalization
or sandbox permission checks were weakened.

The previous incomplete report treated the separate Validate environment as a
blocker to the worker's engineering handoff. The worker's own obligation is to
complete and check this story; the full-suite sandbox check and manual
acceptance remain with Validate. A completed worker result therefore does not
claim that `suite-20-0` was repaired or passed.

The initial complete SQL-heavy package command passed store (375.739s), turn
(1766.013s), and CLI (121.384s), but daemon failed
`TestPlanChildrenSweep_IdleParentReadsNoChildren`: idle tick 1 read the child
twice more. Its trace shows a child dispatched through the independent Plan
backlog path, reading its issue and comments after a parent sweep marked it for
polling. That fixture does not enable conversations. The focused test then
passed (1.726s); no Plan backlog or test expectation was changed. This initial
failure is retained rather than hidden by subsequent checks.

Fresh engineering checks for the following continuation completed successfully:

| Complete package check | Result |
| --- | --- |
| messaging, race, after the meter fix | Passed, 295.317s |
| messaging/discord, race, after the meter fix | Passed, 41.658s |
| store, without race | Passed, 375.739s |
| turn, without race | Passed, 1766.013s |
| daemon, complete rerun, without race | Passed, 327.332s |
| CLI, after the meter fix, without race | Passed, 73.414s |
| command package, race | Passed, 2.509s |

The first complete race command also passed doctor, messaging/ask,
messaging/feature, meterview, projectconnection, repos, rights, and webhookreg.
Messaging/response, resultservice, and store/storetest compiled and have no test
files. Together these are the story's 17 affected packages plus the command
package. Runs used `-count=1 -timeout=0`; no whole-repository test/check target
was run. Messaging, Discord, daemon, and CLI were checked again after the meter
fix because they contain or import the changed package.

The following complete commands checked the changed conversation package and
its runtime importers after the fix:

```text
go test -race ./internal/messaging ./internal/messaging/discord -count=1 -timeout=0
go test ./internal/daemon -count=1 -timeout=0
go test ./internal/cli -count=1 -timeout=0
go test -race ./cmd/litefactory -count=1 -timeout=0
```

`go build ./...`, `gofmt -l .` (empty), and `git diff --check` passed. Pinned
golangci-lint v2.13.1 passed messaging, store, and daemon; messaging was linted
again after the fix, with zero findings. The focused meter follow-up regression
also passed with race detection (4.054s). The complete messaging run includes
that regression and the main-path journey through both providers.

These are worker engineering checks. Manual acceptance, live demonstrations,
and a successful rerun of the retained Validate sandbox suite remain unclaimed.
