# Story-002 engineering handoff — incomplete

Frozen identity: story-002, issue #391 (5581403866), parent #385 (5579648832), revision `6e36e472a8653f63cf8f8a377123a6a8d9e82143bf0189fd71123dd7f5ea9063`; dependency story-001. No acceptance or engineering-completion claim is made.

## Implemented production surfaces

- `internal/buganalysis`: shared pure analysis schema, completeness/ID validation and canonical digests.
- `internal/store/bug_*`, migrations 0066–0069: admitted immutable identity; independent execution revision; eight-pass reservation and dispatch claims; immutable analysis history; authenticated append-only answers; exact published review approval; owned typed repair/check/delivery/failure reference sections. Replays retain their logical identity and reject conflicting evidence. Approval does not allocate an analyst run or spend a pass.
- `internal/agentresult/bug.go` and shared contract changes: version-1 analyst ask/done/approved/blocked, factory-bound invocation/base revision and exact existing approval, strict JSON and typed update targets. Bug results use an empty mode and workflow bug, with additive run/result attribution.
- `internal/turn/bug_record.go`, `bug_analyst.go`, `bug_analysis_policy.go`: concrete record handler; complete retained warm/cold context; guarded shared-runner activity with reservation and claim before execution; successful-run reconciliation; scoped analyst capability registry with typed record-source controls and durable question/review/notice projection.
- `internal/turn/mcp_runtime.go`, `mcp_tools.go`, `internal/submission/tools.go`: a Bug-only broker path exposing exactly communication preflight, submit and receipt. It does not construct unrelated issue, publication, checkpoint or activity tool families.
- `internal/daemon/bug_analysis.go`: mechanical exact `/approve-bug analysis <revision>` reply path under configured numeric owner identity, without analyst or mode selection.
- `teams/source/bug`, `internal/teamsource/bug`, `internal/teamsource/bug.go`: nine-section analyst and result resource closure. Native rendering uses existing isolated package delivery. Claude role inherits the factory-restricted session tools so scoped MCP operations remain reachable.
- Request and adapter guards: standalone identity, explicit analyst profile, exact three MCP result operations; Claude native read tools only; Codex read-only sandbox and shell/delegation disabled. Graph metering accepts Bug workflow with no mode while preserving mode-backed attribution.

## Commands executed by implementation worker

Targeted `go build` commands for the modified production packages passed after fixes. Final `go vet ./internal/buganalysis ./internal/agentresult ./internal/store ./internal/turn ./internal/teamsource ./internal/harness ./internal/codex ./internal/submission ./internal/daemon` passed, including the final policy/daemon additions. No provider launch, live forge call, acceptance/code review, commit or push was performed by this worker. Unit/boundary specialists own tests and their actual results, and the orchestrator owns aggregate make checks.

## Remaining obligations

Actual OS read-only enforcement could not be demonstrated: the separately executed disposable `/usr/bin/sandbox-exec` fixture failed before launching its child with exit 71, `sandbox_apply: Operation not permitted`, under the outer sandbox. This is unavailable enforcement evidence, not a successful denied-write observation. No escalation or paid provider launch is authorized. AC-009 therefore remains incomplete.

The complete executable Bug policy is not added to the global workflow registry. The scoped analysis surface is concrete; general Bug dispatch remains refused until the complete parent package and runtime composition exist. No placeholder graph or borrowed Research handlers were installed. Final production additions require specialist coverage, including scope registration, source checks, exact review rendering/publication/recovery and daemon reply consumption. Complete cross-path question continuation and parent recovery integration remain for the runtime composition; no full end-to-end Bug journey is claimed.

No protected `.codex` installation tree was edited. Source/embedded and temporary rendered installation evidence is distinct from the current protected installation and from actual provider host enforcement. Budgets, shared-result receipt acknowledgment, record application, approval, publication and workflow completion remain separate facts.
