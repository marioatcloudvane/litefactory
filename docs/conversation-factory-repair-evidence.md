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
