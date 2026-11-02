# Codex Bug main-path evidence

After T-04 focused verification passed, ran the automated main path with local test seams:

```
go test -timeout=5m ./internal/daemon -run '^TestCodexBugAgentMainPath$'
```

Final focused output after tightening persisted judge and receipt assertions:

```
ok github.com/cloudvane/litefactory/internal/daemon 5.078s
```

The test enters through daemon issue discovery with a Codex-labelled Bug, executes the concrete Bug runtime against real SQLite and local Git, and submits analyst/fixer results through communication preflight, submit and matching receipt. It asserts:

- Codex receives read-only analyst permissions; no fixer or merge occurs before owner analysis approval.
- A separate no-override JudgeService uses the persisted Codex run provider and saves exactly one Codex judge invocation with its observed model; judging leaves the Bug owner wait unanswered.
- The owner approval releases a Codex workspace-write fixer. A concurrent base edit creates a real Git conflict during factory delivery.
- The registered Codex conflict advisor resolves only the collected conflict regions; a second fixer invocation revalidates the synchronized result before integration.
- Factory delivery creates the PR, merges once and closes once. Every ordinary Bug run remains recorded as Codex.
- Rendered reports include recorded agent, missing-model status and explicit factory-action attribution.

The provider and remote integration service are controlled fixtures; Git and storage are real and local. No production service, live Codex credentials or owner manual acceptance was used.

Scoped `golangci-lint run ./internal/daemon` exited zero with `0 issues.` The restricted environment prevented cache writes under the default linter cache and emitted warnings; lint analysis still completed. The root worker owns the final unified scoped lint and full package runs.
