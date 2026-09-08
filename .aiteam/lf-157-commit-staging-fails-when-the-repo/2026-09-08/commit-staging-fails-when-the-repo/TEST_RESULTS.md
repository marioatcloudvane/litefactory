# Test results — US-1 / #158

Approved revision: `7cc69b218735eac9a403581328344501089d914415b22bb10baf96bbdd552e62`; issue ID `5390210480`; generation 1; run 226. Dependencies: none. Tests derive from the factory-supplied frozen body.

## Results

- `go test ./internal/git -race -cover -count=1`: PASS, 83.9% statement coverage, 45.363s.
- `go test ./internal/git -race -cover -count=1 -run '^TestCommitExcludesFactoryTransport$' -v`: PASS, all six table cases, 34.0% focused coverage, 6.710s.
- Aggregate `GOLANGCI_LINT_CACHE=/private/tmp/lf157-golangci-cache make check`: PASS (exit 0): formatting, vet, pinned golangci-lint v2.13.1 with 0 issues, and full `go test -race ./...`; internal/git 62.433s, internal/turn 223.872s.

## Acceptance mapping

| Criterion | Evidence |
| --- | --- |
| AC-157-1 | Production staging was already corrected by run 2. This run changes no production code. Scoped reviewer verifies exact arguments and unchanged reset/error/result contracts. |
| AC-157-2 | `ignored_nonempty_directory` passes, checking a new commit with ordinary bytes, absent transport in actual tree/index, and unchanged proposal/event bytes on disk. |
| AC-157-3 | `ignored_empty_directory` passes, checking successful ordinary commit, absent transport in actual tree/index, and the still-present empty directory. |
| AC-157-4 | `unignored_transport` explicitly stages both transport paths, resets both, checks that only ordinary.txt remains staged, and checks transport bytes immediately after reset. Public Commit then leaves both paths absent from tree/index and preserves their bytes. |
| AC-157-5 | Entire internal/git suite passes with race detection. Added already-staged ordinary-change and staged/unstaged transport-only cases directly verify public Commit exclusion, successful no-op contracts, HEAD/index, and bytes. Existing status/commit fixture exercises additions, modifications, deletions; provenance tests pass. Aggregate result recorded above. |

## External Git regression verification

Exact binary: `git version 2.50.1 (Apple Git-155)`; fixture execution resolves `/usr/bin/git`. All fixtures are isolated local repositories via existing `internal/gittest`; no forge/network services.

An isolated copy at `/private/tmp/lf157-old-copy-dlunwre8` contains the module files plus internal/git, internal/gittest and internal/naming. Only its commit.go restores the two original exclusion pathspecs. Running:

```sh
cd /private/tmp/lf157-old-copy-dlunwre8
go test ./internal/git -race -cover -count=1 -run '^TestCommitExcludesFactoryTransport$/^ignored_' -v
```

returns exit 1. Both `ignored_empty_directory` and `ignored_nonempty_directory` fail at public Commit because `git add --all -- . :(exclude).litefactory-plan-proposal.json :(exclude).litefactory-story-events/**` exits 1 and names ignored `.litefactory-story-events`. This supplies the negative control without editing production files. An earlier Go overlay attempt unexpectedly passed and is not used as negative-control evidence.

To reproduce after upgrading Git, run `git --version`, the two passing commands above, and create a fresh scratch copy outside the repository using this Python snippet from the worktree root, then run the negative-control command in the printed directory:

```python
import pathlib, shutil, tempfile
root = pathlib.Path.cwd()
scratch = pathlib.Path(tempfile.mkdtemp(prefix="lf157-old-copy-", dir="/private/tmp"))
for name in ["go.mod", "go.sum"]:
    shutil.copy2(root / name, scratch / name)
for name in ["git", "gittest", "naming"]:
    shutil.copytree(root / "internal" / name, scratch / "internal" / name)
p = scratch / "internal/git/commit.go"
source = p.read_text()
current = '"add", "--all", "--", "."'
original = current + ', ":(exclude).litefactory-plan-proposal.json", ":(exclude).litefactory-story-events/**"'
assert source.count(current) == 1
p.write_text(source.replace(current, original))
print(scratch)
```

The positive unignored case executes exactly `git add --all -- .`, `git diff --cached --name-only`, `git reset --quiet HEAD -- .litefactory-plan-proposal.json .litefactory-story-events`, and `git diff --cached --name-only`; it then inspects disk bytes, public Commit, `git ls-tree -r --name-only HEAD -- <transport paths>`, and `git ls-files -- <transport paths>`.

## Scope

Test edits: internal/git/worktree_test.go only. No dependencies, integration tags, production changes, commit or push. Suggested immutable evidence snapshot: internal/git/commit.go, internal/git/worktree_test.go, this validation report, and the final scoped review report.
