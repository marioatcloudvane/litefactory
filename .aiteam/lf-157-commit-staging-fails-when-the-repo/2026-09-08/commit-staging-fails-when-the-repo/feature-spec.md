# Feature Spec: commit-staging-fails-when-the-repo
# Status: APPROVED
# Date: 2026-09-08

## Goal
- Let litefactory commit ordinary task changes successfully when a repository ignores the story-event transport, while keeping both factory transport paths out of commits.

## User Stories
| ID | Story |
|----|-------|
| US-1 | As a repository owner, I want task changes committed successfully when the story-event directory is gitignored so completed agent work is saved without including factory transport files. |

## Data Model
| Entity | Change |
|--------|--------|
| Persistent entities and schema | None. |
| Existing transport files | Preserve `.litefactory-plan-proposal.json` and `.litefactory-story-events/` on disk; exclude their changes from the index and commit. |

## API / Interface Changes
| Interface | Change |
|-----------|--------|
| `Worktree.Commit` | Preserve signature and result contract; remove the ignored-transport staging failure. |
| CLI, configuration, permissions and input validation | None. |

## Business Rules
- Limit production changes to the staging invocation in `internal/git/commit.go`: use `git add --all -- .`, removing both transport exclusion pathspecs.
- Retain the existing transport reset operations and their error handling; they remove both transport paths from staging.
- Continue committing the same ordinary modifications, additions and deletions as before.
- Continue returning genuine staging/reset/commit failures; do not broadly suppress exit status 1.

## Edge Cases & Error Handling
- `/.litefactory-story-events/` is gitignored and contains an event file: ordinary changes commit successfully, and neither transport path enters the commit.
- `/.litefactory-story-events/` is gitignored and exists as an empty directory: ordinary changes commit successfully, matching the persistent directory left after run cleanup.
- Neither transport path is gitignored: staging may add both paths, but the existing reset removes both from the index without deleting their files.
- Transport files were already staged before `Commit`: retain the existing exclusion behavior.
- Only transport changes exist: preserve the existing successful no-commit result.

## Out of Scope
- Story-event cleanup, daemon outcome reporting, managed story transport protocols and GitHub publication.
- Changes to `.gitignore`, Git advice configuration, public APIs, dependencies or permission policy.
- Refactoring the commit flow or changing the set of ordinary files committed.

## Acceptance Criteria
- [ ] AC-157-1: The staging invocation is `git add --all -- .` with neither transport exclusion pathspec, and existing transport reset handling remains in place.
- [ ] AC-157-2: A local fixture with `/.litefactory-story-events/` in `.gitignore`, a nonempty event directory and an ordinary file change returns a successful committed result; the resulting commit includes the ordinary change and neither transport path.
- [ ] AC-157-3: The same ignored-directory fixture with an existing empty event directory commits the ordinary change successfully and includes neither transport path.
- [ ] AC-157-4: A fixture that ignores neither transport path demonstrates that `add --all -- .` stages both transport paths, and the existing reset unstages both while preserving their files on disk; `Worktree.Commit` commits ordinary changes without either transport path.
- [ ] AC-157-5: Relevant `internal/git` regression checks and `make check` pass with no change to ordinary commit behavior.

## Assumptions and Sources
- Source: parent issue `github:marioatcloudvane/litefactory#157` and its owner comment requesting Plan; the earlier `/spike` wording describes the reported issue, while this run produces planning artifacts only.
- Source: `.aiteam/APP_CONTEXT.md` identifies `internal/git` as the Git owner and `internal/gittest` as the existing offline fixture helper.
- Source: `internal/git/commit.go` already resets both transport paths before reading status and again after staging; only the staging arguments need production modification.
- Assumption: one story contains the bounded staging fix and its regression coverage, because they jointly establish the requested behavior.
- Assumption: no existing child-story identities were supplied, so planning creates a new story and invents no issue identity or base revision.
- Owner questions: none.
