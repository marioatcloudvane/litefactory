---
name: go-stack
description: Go test, race, timeout, format and lint rules for the checks a role runs.
---
Apply these rules together with the supplied role. This skill grants no tools or authority.

# Tests and linters

Roles that run tests and linters (story worker, sync resolver, bug fixer, Validate repair) use these rules for their scope. The repository's own Makefile, CI and project instructions win where they say more.

- While fixing, run only the tests you are working on, with `go test -run <pattern>` on that package.
- A complete package run gets a finite `-timeout` sized to the package, for example `-timeout=30m` for a package that takes 15 to 20 minutes; never `-timeout=0`.
- Each complete package run uses `-race`, except the packages the repository's own rules run without it because their race runs take far too long; Validate covers those. Never run the same package with and without `-race` at the same time.
- Format and lint the packages you changed with the repository's tools, usually `gofmt` and `golangci-lint run` on those packages. When `golangci-lint` is not on `PATH`, use the repository's lint target (for example `make lint`), which may run the pinned linter through `go run`; a missing binary is no reason to skip lint.
- The agent sandbox refuses to create Unix sockets, so a test that binds one fails with `operation not permitted` or `cannot create socket` there. That failure is the sandbox, not your change. Name each test or package that could not run for that reason in your report, and never count it as passed.
- Never run the repository's full check or full suite yourself: not `make check`, not `scripts/lf-check.sh` or `scripts/ci-test.sh`, not the browser check, not a Docker image build of the project. Run only the tests for the files you changed (the failing test you wrote, red then green), with the project's own test command and a bounded timeout. Validate runs the full check once, after you finish.
