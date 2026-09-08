# Implement notes — run 0003

## Approved scope

US-1 / GitHub #158, numeric issue ID 5390210480, generation 1, revision `7cc69b218735eac9a403581328344501089d914415b22bb10baf96bbdd552e62`; no dependencies. The factory-supplied inline frozen body is the specification. Existing Spike work is inspected and reused where it already satisfies the approved scope.

## Execution

Implementation inspection → scoped review → offline fixture tests and any missing coverage → final review of changed validation files → acceptance evidence publication → aggregate completion. No infrastructure provisioning is needed for isolated local Git fixtures.

## Boundaries

No questions, forge calls, commits, pushes, release builds, runtime-state changes or local story Markdown. Evidence is published atomically in `.litefactory-story-events/226` and left for the factory.

## Initial review and resolution

Production inspection found the existing fix already satisfies T-157.1, with no production edits needed. Initial review CR-B-001 identified missing direct public-operation checks for already-staged transport and transport-only successful no-op results; the test engineer owns that bounded AC-157-5 gap.

## Environment and context

`go build ./...` passed in this run. Local offline Git fixtures need no services or integration tags. The context updater found APP_CONTEXT.md and APP_CONSTRAINTS.md remain accurate: no new API, dependency, boundary or normative rule was introduced, so neither document was changed.

## Final scoped review

CLEAN: CR-B-001 resolved. Direct public-operation tests now cover already-staged transport alongside an ordinary change and staged/unstaged transport-only successful no-ops. Isolated reset byte preservation is asserted immediately. Production remained unchanged throughout this run.

## Regression reproduction

The test engineer reproduced both ignored-directory failures with the original arguments in a scratch copy using Git 2.50.1 (Apple Git-155), while the actual worktree retained the fix. The focused full internal/git race/coverage run passed (83.9% statement coverage). An initial Go overlay experiment did not reproduce the failure; the scratch-copy experiment supplies the decisive negative result.

## Final aggregate gates

`GOLANGCI_LINT_CACHE=/private/tmp/lf157-golangci-cache make check` passed: formatting, vet, pinned lint (zero issues) and full race suite. All six transport cases passed. Final scoped review is CLEAN; there are no unresolved blockers or drift. Implementation and validation files are frozen for verification publication.
