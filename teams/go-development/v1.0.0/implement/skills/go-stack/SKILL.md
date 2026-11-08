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
- Format and lint the packages you changed with the repository's tools, usually `gofmt` and `golangci-lint run` on those packages.
- Never run the repository's full check or full suite yourself: not `make check`, not `go test ./...` over the whole module, not a CI script, not a Docker image build of the project. Run only the tests of the packages in scope, with `go test` and a finite `-timeout`. Validate runs the full check once, after you finish.
