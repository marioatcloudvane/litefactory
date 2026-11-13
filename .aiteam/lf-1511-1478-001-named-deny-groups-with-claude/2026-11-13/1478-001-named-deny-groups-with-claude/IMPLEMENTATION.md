# Named deny groups — implementation evidence

The story adds optional operator deny catalogues, static and configured graph selection validation, per-attempt resolution, Claude native delivery and defensive Codex refusal. Three subagents built catalogue/validation, provider delivery and end-to-end/lifecycle tests in parallel; the lead implemented attempt handoff and the five operator guides and reviewed every change.

## Delivered contract

- Ordered graph-first union, exact deduplication, empty node selections retaining defaults, strict YAML and catalogue validation, explicit agent activity membership, and provenance-bearing errors.
- Current configuration resolved once for each new/resumed attempt. Independent request clones retain the snapshot through capacity retries and refused-resume cold fallback. Unknown names and reload failures refuse before agent execution.
- Claude compiles supported native Bash, Read, Edit and Agent rules. Relative file rules anchor at the actual launch worktree while retaining native matching depth. Existing source-relative file permission rules are rebased before their settings document moves; native negation remains unchanged. Unsupported selected negation, parameter/glob forms and ambiguous generated anchors refuse.
- Unique private atomic settings files add selected and shared denies, including launches without Bash. Existing allow/ask lists and sandbox confinement remain intact; unsupported enforcement-helper and proxy overrides refuse. Artifacts are checked against writable roots, including resolved symlink paths, and are independently cleaned up.
- Graph agent dispatch, direct Validate reviewer/reporter launches, and graph advisory judges all carry the snapshot. Graph judges retain their isolated Read/Grep/Glob ceiling and do not import ordinary operator allow settings. Standalone judge, doctor and conversation behavior stays unchanged.
- Codex selected launches, including graph judges, refuse before process creation. No new Codex home, authentication, resume, usage or sandbox changes were introduced. Factory-run checks and shipped teams remain unchanged.
- Five guides provide settings/local-team examples, selection and matching rules, freshness, refusal recovery and the temporary Codex limit.

## Verification

All required scoped checks passed. Toolchain: Go 1.27.0 (the module declares Go 1.26), pinned golangci-lint v2.13.1.

- Complete policy leaf check: PASS (1.522s), included in the actual initial command `go test -race -timeout=30m ./internal/denypolicy ./internal/config ./internal/agent ./internal/graphexec ./internal/harness ./internal/codex ./internal/teamsource ./internal/teampackage ./internal/teambuild`. The policy leaf source did not change later. The original group command returned failure for the template/default empty-map mismatch, which was fixed; this is not a pre-existing failure. The final group below supersedes that configuration failure.
- Final complete race-enabled group: `go test -race -timeout=20m ./internal/config ./internal/agent ./internal/graphexec ./internal/harness ./internal/codex ./internal/teamsource ./internal/teampackage ./internal/teambuild`: PASS for all eight packages.
- Final complete dispatch group: `go test -timeout=40m ./internal/turn ./internal/daemon ./internal/cli`: PASS (735.870s, 170.410s, 43.527s). SQL-heavy packages follow the worker's no-race exceptions.
- Focused launch/lifecycle/judge command: `go test -run 'Test(GraphDenyGroupsClaudeLaunch|DenyAttempt|GraphJudgeDeny)' -timeout=5m ./internal/turn`: PASS, also included in the final complete dispatch run.
- `go build ./...`: PASS. No output was written into `dist/`.
- `GOLANGCI_LINT_CACHE=/tmp/lf1511-checks/lint-cache make lint PKGS='./internal/denypolicy ./internal/config ./internal/agent ./internal/graphexec ./internal/harness ./internal/codex ./internal/turn'`: PASS, zero issues.
- `gofmt -l .`, `git diff --check`, five-guide local link/heading checks and shipped-team diff review: PASS.

No final failures, known pre-existing failures or socket-blocked packages remain. All twelve packages in scope received complete checks. Full module tests/checks were left to Validate.

The controlled end-to-end test is `TestGraphDenyGroupsClaudeLaunch`. It loads operator YAML and a DOT graph, dispatches through `ExecuteNodeAction` and `AgentActivity`, and invokes the real Claude adapter with a controlled CLI process that copies the actual settings file before cleanup. It covers additive selections, duplicate removal, empty node defaults, shared protections, existing allow/ask/deny preservation and delivery without Bash.

Focused lifecycle tests exercise fresh configuration, reload refusal with zero runner calls, adapter mutation isolation, capacity retries, resumed cold fallback, graph judge fallback, and no-selection behavior. Provider tests cover compiler support and anchors, strict settings, sandbox preservation, cancellation, destination failure, private artifact location, concurrent isolation, and Codex refusal. Configuration tests also check that credential-shaped entries and names never appear in diagnostics.

Documentation was reviewed semantically; local links/headings resolve in all five changed guides. No shipped team gained a group reference. No wording/hash pins or role prose changes were added. No production endpoint, live model run, or full repository test/check command was used.

## Decisions recorded for review

1. Permit configuration and graph validation to import one stdlib-only policy leaf, with narrow import guards and a leaf-dependency test, rather than duplicate parsers. Locations: `internal/config/purity_test.go:63`, `internal/graphexec/purity_test.go:38`.
2. For graph judges, append shared and selected denies to their existing isolated provider settings instead of importing ordinary operator permissions, which could widen their readable roots. Location: `internal/harness/deny_policy.go:182`.

No O-/N-/K- outcome ruling departs from the approved story. Codex enforcement remains deferred exactly as approved; selected-group refusal is not enforcement delivery.

## Evidence limits and standards

Owner check pending: AC-002, AC-003, AC-004, AC-005, AC-006. Controlled processes prove factory delivery, not live Claude native matching, permitted descendant inheritance or installed-version behavior. Validate owns acceptance and code/architecture review; engineering completion does not establish acceptance or publication.

The encountered pre-existing Go-embedded delegation instruction remains at `internal/turn/research_agent.go:539`, injected through `implementAttemptGuidance`. It is the agent-guidance standards violation already recorded in planning and was not changed or moved by this story. No new behavioral instruction prose was added in Go.

An aggregate-capture preflight was refused with `store: aggregate requires exact Implement binding and bounded evidence`. No aggregate was submitted or retained through that refused preflight. Final reporting uses this node's prescribed empty-stories result route and retains the actual check logs here.
