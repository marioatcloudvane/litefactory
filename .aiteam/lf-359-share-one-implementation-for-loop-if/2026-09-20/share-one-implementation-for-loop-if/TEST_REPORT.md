# Engineering test report

Final outcome: all selected engineering work and required aggregate checks passed. Evidence is offline only; acceptance, remote effects and factory save remain separate.

## Final aggregate checks

- `GOLANGCI_LINT_CACHE=/private/tmp/lf359-golangci-cache make check` exited 0. The temporary cache path only accommodates filesystem permissions. The repository target ran formatting, vet, pinned golangci-lint v2 and the full race suite.
- `go build ./...` exited 0 with empty output. The only later implementation change was the AST test fixture, which does not affect production build inputs.
- `gofmt -l .` exited 0 with empty output (0 bytes), after the final test change.
- All 53 current implementation/test/documentation file hashes were rechecked after the aggregate run and match retained story snapshot digest `280bfefd826aeb5af6da03ec3b50e5dbb44cfcd9aa6c3b0b372281c1024f8c5f`.

Actual final make check output:

```text
go vet ./...
golangci-lint v2.13.1 (installed)
0 issues.
go test -race -timeout=0 ./...
ok  	github.com/cloudvane/litefactory/cmd/litefactory	(cached)
?   	github.com/cloudvane/litefactory/cmd/team-benchmark	[no test files]
?   	github.com/cloudvane/litefactory/cmd/team-publish	[no test files]
ok  	github.com/cloudvane/litefactory/internal/agent	(cached)
ok  	github.com/cloudvane/litefactory/internal/agentresult	(cached)
ok  	github.com/cloudvane/litefactory/internal/attach	(cached)
ok  	github.com/cloudvane/litefactory/internal/cli	(cached)
ok  	github.com/cloudvane/litefactory/internal/codex	(cached)
ok  	github.com/cloudvane/litefactory/internal/config	7.061s
ok  	github.com/cloudvane/litefactory/internal/confirm	(cached)
ok  	github.com/cloudvane/litefactory/internal/daemon	972.944s
ok  	github.com/cloudvane/litefactory/internal/git	(cached)
ok  	github.com/cloudvane/litefactory/internal/github	(cached)
ok  	github.com/cloudvane/litefactory/internal/gittest	6.158s
ok  	github.com/cloudvane/litefactory/internal/graphexec	1.958s
ok  	github.com/cloudvane/litefactory/internal/harness	(cached)
ok  	github.com/cloudvane/litefactory/internal/identity	10.729s
ok  	github.com/cloudvane/litefactory/internal/launchd	(cached)
ok  	github.com/cloudvane/litefactory/internal/logging	(cached)
ok  	github.com/cloudvane/litefactory/internal/mcpadapter	(cached)
ok  	github.com/cloudvane/litefactory/internal/meter	(cached)
ok  	github.com/cloudvane/litefactory/internal/naming	(cached)
ok  	github.com/cloudvane/litefactory/internal/planbrief	(cached)
ok  	github.com/cloudvane/litefactory/internal/redact	(cached)
ok  	github.com/cloudvane/litefactory/internal/render	(cached)
ok  	github.com/cloudvane/litefactory/internal/report	(cached)
ok  	github.com/cloudvane/litefactory/internal/repos	(cached)
ok  	github.com/cloudvane/litefactory/internal/schedule	(cached)
ok  	github.com/cloudvane/litefactory/internal/secrets	(cached)
ok  	github.com/cloudvane/litefactory/internal/store	(cached)
ok  	github.com/cloudvane/litefactory/internal/submission	(cached)
ok  	github.com/cloudvane/litefactory/internal/team	(cached)
ok  	github.com/cloudvane/litefactory/internal/teampublish	7.606s
ok  	github.com/cloudvane/litefactory/internal/teamremote	(cached)
ok  	github.com/cloudvane/litefactory/internal/teamsource	27.927s
ok  	github.com/cloudvane/litefactory/internal/turn	2606.268s
```

## Final scoped engineering checks

The following execution, after all code and fixture corrections, backs both current v3 story snapshots:

```text
go test ./internal/turn ./internal/graphexec ./internal/teamsource ./internal/store ./internal/daemon ./internal/agent ./internal/harness -run '^(TestResearch.*|TestShaping.*|TestShipped.*|TestCaptureResearch.*|TestClaimGraphOwnerReply.*|TestGraphPoll.*|TestGraphStep.*|TestSharedControlsUseAdmittedVocabularyAndInitialDefault|TestWorkflowReuse.*|TestWorkflowMainRolePreservesPermissionsAcrossLaunchPaths|TestRequest_GraphNodeIdentity|TestExecuteNodeActionHasOnlyResearchProductionCaller)$' -count=1 -timeout=8m
ok  	github.com/cloudvane/litefactory/internal/turn	84.623s
ok  	github.com/cloudvane/litefactory/internal/graphexec	0.928s
ok  	github.com/cloudvane/litefactory/internal/teamsource	1.634s
ok  	github.com/cloudvane/litefactory/internal/store	4.061s
ok  	github.com/cloudvane/litefactory/internal/daemon	6.186s
ok  	github.com/cloudvane/litefactory/internal/agent	1.709s
ok  	github.com/cloudvane/litefactory/internal/harness	3.647s

go test ./internal/graphexec -run '^TestShared' -count=1 -timeout=2m
ok  	github.com/cloudvane/litefactory/internal/graphexec	0.356s
```

## Behavior evidence

- Shared generic action authorization and explicit mode fences; removed mechanism names; alternate vocabulary; initial source defaults and durable wait path rejection.
- Role delivery and permissions across cold, resume and refused-resume fallback; attempt/run and metering identity; retained publication identity, acknowledgment-loss reconciliation and authenticated owner waits/restart.
- Real filesystem destination prevalidation, partial-write failure retention, deterministic document replay, generated-file exemptions and current candidate/document gating.
- Assembled Research rejects worker-invented approval, changed candidates after approval and authenticated revision requests misrepresented as approval. Genuine owner approval completes once. Both parked handoff and allocated reply recheck current generated documents before another provider invocation.
- Test-only Plan package reuses capture, Markdown delivery, generic activities, durable wait/restart and later file-added role execution without Research document generation or production Plan registration.
- AST structure check enforces one shared dispatcher and one Research-only production entry with its explicit policy.

## Failures found and corrected

Scoped runs exposed old fixture vocabulary/generation assumptions and shared-runtime metering/historical-wait assumptions. Assembled tests exposed invented and changed-candidate approval; all were corrected and final scoped execution passed.

One early combined unit regex accidentally selected unrelated shared-finalization tests and was interrupted at 237.419s. It is not passing evidence. The final full race run covers those tests successfully.

Aggregate fix cycle 1 corrected 30 exported API comments, seven diagnostic capitalization issues and one test slice construction. The following full race run passed all packages except the obsolete AST dispatch-owner expectation (turn 2699.854s). Fix cycle 2 updated that expectation while preserving the production fence; its exact race check passed 4.048s and the final whole make check passed, with turn 2606.268s. No unresolved failures remain.

## Retained current story evidence

- US-001: `run563-us001-v3`, check `us001-final-scoped-v3`; receipt request `49c059bd59f7337db4cb06a3abbd2863`.
- US-002: `run563-us002-v3`, check `us002-final-scoped-v3`; receipt request `e8c6fd4fd80a74931a232ba13309b8aa`.
- All current preflights were valid and complete; matching receipts were read. Input valid, engineering recorded, acceptance not applicable, remote effects pending.
- Earlier unaccepted snapshots were explicitly invalidated before replacement; historical execution and causes remain retained.
- Aggregate capture includes every final changed worktree path, including these reports. Its final receipt is returned through the application result handoff rather than changing captured files afterward.
