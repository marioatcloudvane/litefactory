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
- The repository's full check (for example `make check`) belongs to Validate; a story worker, sync resolver or bug fixer does not run it.
