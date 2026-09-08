# Spike — commit-staging-fails-when-the-repo

2026-09-08. Run 0002 of github:marioatcloudvane/litefactory#157 requested the single staging-call fix and regression coverage, including confirmation that reset still excludes unignored transport.

## What was done

Changed only the production staging arguments in `internal/git/commit.go` to `git add --all -- .`. Both existing transport resets and their error handling remain unchanged.

Added `TestCommitExcludesFactoryTransport` in `internal/git/worktree_test.go`, with isolated offline Git fixtures for ignored nonempty and present-empty event directories, and unignored transport. The tests inspect the actual commit and index, verify the ordinary file was committed, and check transport bytes or the empty directory remain on disk.

## What was found

Verified with `git version 2.50.1 (Apple Git-155)`. Before the production change, the new test failed in both ignored-directory cases with Git exit status 1 naming `.litefactory-story-events`. After the change, all three cases passed.

The unignored case explicitly executes `git add --all -- .`, verifies both transport files are staged, then executes `git reset --quiet HEAD -- .litefactory-plan-proposal.json .litefactory-story-events` and verifies only the ordinary file remains staged. It then exercises the public commit operation and checks neither transport path is in the commit or index, while file bytes remain intact. These Git writes occur solely in temporary test fixtures outside the task repository.

Re-verify after a Git upgrade with `git --version` and `go test ./internal/git -run '^TestCommitExcludesFactoryTransport$' -count=1 -v`. Run `make check` for the aggregate formatting, vet, pinned lint and race-test gates.

Aggregate validation: `make check` passed (formatting, `go vet ./...`, pinned golangci-lint v2.13.1 with zero issues, and `go test -race ./...`). The linter emitted sandbox cache-write warnings but completed successfully. `git diff --check` also passed.

## Decisions worth remembering

The existing post-staging reset supplies transport exclusion without explicitly naming ignored paths in `git add`. Removing the directory, suppressing advice or swallowing staging errors is unnecessary. The tests use actual Git state because filtered worktree status deliberately hides transport.

## Not done

No changes to cleanup, daemon outcome reporting, ignore rules, permissions, public contracts or unrelated code. No release build, runtime-state edits, commit or push was performed.

## Recommendation

Review and merge this validated bounded fix. No further Research, Plan or Implement work is needed for the requested scope.
