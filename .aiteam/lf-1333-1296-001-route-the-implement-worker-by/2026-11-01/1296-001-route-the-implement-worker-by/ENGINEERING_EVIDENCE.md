AI;DR: New captures route from immutable approved sizing through DOT, with selected tier and configured model retained. Automated engineering evidence is recorded here; Validate owns acceptance and the live L-story observation.

Three subagents built sizing persistence and validation, graph admission, and the end-to-end tests/owner handoff in parallel. The primary worker connected Plan publication, the fact adapter, generated packages, caller fixtures and final package checks.

The approved sizing snapshot is optional for old records. Publication checks the exact retained Plan revision, promoted set digest and mapping, issue and projection identities. Approval copies those facts transactionally and rechecks the final assembled, published and saved revision plus unchanged raw sizing. Later plans cannot supply a missing snapshot or replace an existing one. Source metadata survives ordinary archive and follows approved scope only during explicit family forgetting. Read/decode/cancellation failures remain errors.

The runtime exposes only size and validity. DOT owns S/M role-tier inheritance and the flagship fallback. Admission requires equivalent workers behind one budget and synchronization gate, a closed acyclic fact region, isolated results and a common save/park flow. Existing single-worker captures remain valid. The new end-to-end tests cover 44 cases: all four generated teams, S/M/L, absent and invalid sizing, a changed captured role default, retained provenance and save, repair/restart after later Plan changes, absent snapshots, old captures, failure/incomplete/blocked outcomes, save retry, owner restart, deferred conflicts and selected capacity. Existing common-gate tests cover retry/conflict bounds.

Ruling: O-08 — Generated four 1.0.1 packages instead of overwriting 1.0.0 — the existing generator refuses immutable release replacement; this preserves old captures and catalogue entries — owners must install/link the new version for new routing (teams/source/go-development/release.json:4).

All four old catalogue entries compare unchanged to the starting commit, and no 1.0.0 release files changed. Generated 1.0.1 packages and the combined manifest were produced with the existing generator. Worker prompts were not edited.

Executed complete package commands (finite timeouts; SQL-heavy exceptions use no race instrumentation):

```sh
go test -race -timeout=20m ./internal/graphexec ./internal/planbrief ./internal/teamsource ./internal/teamrelease ./internal/agentresult ./internal/rights ./internal/taskdiscussion ./internal/teampackage ./cmd/teamrelease
go test -race -timeout=20m ./internal/graphexec
go test -timeout=25m ./internal/store
go test -timeout=20m ./internal/daemon
```

The first command passed every package except graph execution; the command package contains no test files. After updating only the changed Implement source identity and the legacy stopped-shape fixture, the complete graph rerun has only the five pre-existing failures listed below. Complete store and daemon reruns passed after classifying the new provenance table for archive/forget and providing the flagship model in an unsized-worker scheduler fixture. Initial failed checks remain failed historical observations; their passing reruns apply after those fixes.

The final complete `go test -timeout=30m ./internal/turn` rerun passed in 789.833 seconds. Its initial complete run had failed three fixture assumptions: `TestImplementReleasedGraphStillParksStoppedAtOnce`, `TestPreparationCaveatsContinueOriginalRequest/canonical_managed_worker_reaches_approved_work_through_deferred_sync`, and `TestStandardsGrantDeclaredOnlyForThreeRoles`. The fixes use retained result-node identity, expect flagship for unavailable sizing, and compare unique granted roles across exclusive paths. The final full run covers the final Go implementation and all updated turn fixtures.

Focused new routing checks passed with `go test ./internal/turn -run '^TestImplementSizing' -timeout=8m`; the strengthened three-launch repair case was rerun and passed separately. Focused checks for the three old single-worker assumptions found by the first complete turn run passed after their fixture updates. Store/graph/validation boundary checks also passed while building. The new publication revision guard was added to Makefile’s targeted store race selection and passed `go test -race -timeout=3m ./internal/store -run '^TestPublishedStorySizingConcurrentPlanRevisionGuard$'`. Retained logs in `checks/` record package summaries and graph diagnostics; the old turn failure log is summarized here rather than retaining its dumped temporary runner prompt.

Other executed checks passed:

```sh
go build ./...
gofmt -l .
git diff --check
go run ./cmd/teamrelease -check -source teams/source/go-development -source teams/source/python-fastapi -source teams/source/dotnet-web -source teams/source/swift-ios
```

Pinned golangci-lint v2.13.1 passed for every changed Go package: graphexec, planbrief, store, teamsource, turn and daemon. Writable temporary linter caches were used. Later fixture/archive fixes received their own scoped lint runs. The formatter reported no files. The generator check validated all four candidate packages, resource closure and manifest integrity. The whole test suite and repository full check were not run. Environment: Go 1.27.0, darwin/arm64.

Pre-existing failures, left unchanged: `TestExtractionRetainsShippedAndLegacyGraphIdentities` for these five Validate graphs:

- `teams/source/base-team/validate/validate.dot`
- `teams/go-development/v1.0.0/validate/validate.dot`
- `teams/dotnet-web/v1.0.0/validate/validate.dot`
- `teams/python-fastapi/v1.0.0/validate/validate.dot`
- `teams/swift-ios/v1.0.0/validate/validate.dot`

They were reproduced with only that failing test at starting commit `4b58f1675f594a2b8c3988890d3241790079b2d6`, extracted to a temporary scratch directory without switching branches. All five identity diagnostics match the final graph run exactly. The scratch directory was deleted. See `checks/graph-starting-commit.log` and `checks/graph-final.log`.

Existing agent-guidance standards violations remain for the operator: embedded worker instructions/hard rules in `internal/turn/implement_policy.go:17` and `:19`, and bootstrap behavior guidance in `internal/turn/research_delivery.go:73`. They were not moved or extended; prompt changes are excluded by this story.

Owner check pending: AC-002, AC-003, AC-004, AC-005, AC-006. No live provider invocation, production test, operator installation/linking, deployment or owner-observed L run was performed. The configured-model assertions use local synthetic runners and prove delivery/provenance behavior, not real provider execution. Follow [the L-story handoff](L_STORY_HANDOFF.md) for the owner's live observation. The story branch was not staged, committed, pushed or merged by the worker. Test commits and pushes used isolated local Git fixtures.
