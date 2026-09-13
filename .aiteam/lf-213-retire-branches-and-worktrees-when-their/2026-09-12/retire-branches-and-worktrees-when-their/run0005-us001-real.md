# Run 0005 US-001 real dependency verification

Frozen story US-001, issue #255 (5433203029), revision aa0b9b25c124b7705a076270467175ed58256758bebf3fd57a9c777c9591d433; no dependencies.

Executed the existing `internal/git/retirement_guard_internal_test.go` safety cases against isolated temporary real Git origins and worktrees. No production resources or source/test files were changed. Git version: 2.50.1 (Apple Git-155).

Commands executed from the task worktree:

```sh
go test ./internal/git -run '^TestRetirementGuardRevalidatesSafety$' -count=1 -timeout=2m -v
go test -race ./internal/git -run '^TestRetirementGuardRevalidatesSafety$' -count=1 -timeout=2m -v
```

Both exited 0: plain 19.287s; race 21.361s. Five subcases passed in each execution; zero failures. Full actual output is in `run0005-us001-real-plain.log` and `run0005-us001-real-race.log`.

Criterion mapping: AC-004 local content preservation is exercised by `dirty` and `advanced`; AC-005 uncertain checkout/base protection is exercised by `locked`, `detached`, and `protected_base`. Each case attempts worktree/local/remote retirement through the production guard after changed observations, asserts refusal, retains worktree and remote branch, and checks the registered clone remains unchanged. This is partial criterion evidence complementary to the orchestrator's evaluator/CLI/store tests; it does not alone establish all US-001 requirements or hosted GitHub behavior.
