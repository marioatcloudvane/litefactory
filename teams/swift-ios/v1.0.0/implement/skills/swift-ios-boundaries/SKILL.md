---
name: swift-ios-boundaries
description: Swift iOS workflow, host and evidence boundaries.
resources:
  - boundaries.md
---
Apply boundaries.md together with the supplied role. This skill grants no tools or authority.

# Tests and linters

Roles that run tests and linters (story worker, sync resolver, bug fixer, Validate repair) use these rules for their scope. The repository's own Makefile, CI and project instructions win where they say more.

- Run the repository test targets in the scope with its existing XCTest or Swift Testing conventions and the admitted Swift profile, filtered while fixing and completely once at the end with a finite timeout. Keep unsigned simulator checks scoped to the story or resolution.
- Run the repository's existing Swift formatter and linter on the changed scope. Follow its pinned tools and configuration; host tool installation requires operator preparation.
- Never run the repository's full check or full suite yourself: not `make check`, not every test target of the scheme, not a CI script, not a UI or browser check outside your scope. Run only the test targets in scope, with the admitted Swift profile and a bounded timeout. Validate runs the full check once, after you finish.
