# Story-003 worker proof

Engineering record for the prepared story branch. No acceptance, publication,
commit, push or hosted-client success is claimed. Three subagents implemented
consent/Git safety, compatibility/standards projections, and the main-path
integration test/guidance/documentation. The lead reviewed their changes and
ran integrated package and caller checks.

## Delivered behavior

Explicit discussion plus code-review consent binds an immutable retained task,
code connection and repository. Ticket and code repositories may differ.
Authorization checks cover discovery, acquisition, continuation and release,
including lifecycle changes and parent mappings with ticket-origin identity.

Six read-only evidence tools expose locally committed task/base file reads,
search, reachable history, direct-tree or first-parent changes, verified
published standards, and separately labeled selected-category/whole-book role
counts. Authenticated view/cursor tokens preserve versions and expire within
15 minutes or the grant lifetime. No arbitrary revision, working-tree read,
fetch, helper execution, standards write or workflow mutation is exposed.

Repository and book acquisition is bounded before allocation/disclosure.
Complete source units are sanitized before paging. Unsafe diff source or search
line provenance produces an explicit gap. Source links use a recognized retained
remote; local mirrors receive named unavailable-link evidence. Summaries and
complete serialized responses use the prerequisite limits. Initialization
Markdown is selected only for installed tools and both live scopes.

## Compatibility sequence and main path

Characterization tests were run before the shared reader/projection extraction,
then rerun through migration. Legacy analyst/standards-author public contracts
remain. Superseded reader dispatch and standards verification/filtering logic
were replaced with shared implementations.

The one main-path test exercises real local browser-consent/OAuth/MCP handlers,
a migrated temporary SQLite database, temporary committed Git fixtures and a
verified published book. Only upstream owner identity verification is synthetic.
It checks all six tools, exact source references, task/base selection, old-view
pinning versus new observations, standards paging and counts, limits, refusal,
revocation and unchanged working-tree state. The example comparison is recorded
in CODE_STANDARDS_EXAMPLE_CHECK.md. These are local protocol checks.

## Final checks

The repository-wide test suite and full check were not run. Complete scoped
runs used the repository's race policy: race for ordinary packages, no race for
SQL-heavy store, daemon, turn and CLI. Every invocation had a finite Go timeout
and every background run also had a finite external timeout.

Executed complete commands, with successful runs retained in this session:

```sh
go test -race -timeout=30m ./internal/connector ./internal/codeevidence ./internal/git ./internal/projectread ./internal/standards ./internal/drift ./internal/loganalysis ./internal/standardsexceptions
go test -timeout=40m ./internal/store ./internal/daemon ./internal/turn ./internal/cli
go test -timeout=40m ./internal/store ./internal/daemon
go test -race -timeout=30m ./internal/connector ./internal/codeevidence
go test -race -timeout=30m ./internal/git ./internal/projectread ./internal/codeevidence ./internal/loganalysis
go test -race -timeout=15m ./internal/git ./internal/projectread
go test -timeout=30m ./internal/turn ./internal/cli ./internal/daemon
```

The first SQL group exposed the new table's missing archive classification; that
story-caused issue was fixed. The affected store and daemon packages passed on
subsequent complete runs. Turn and CLI passed the first group. Later full runs
followed the final code changes. The final caller group passed: turn 721.488s,
CLI 40.179s and daemon 195.134s. All twelve packages in scope have successful
complete runs, including relevant importers. No pre-existing test failure is claimed.

Latest completed race results: connector 39.843s; codeevidence 42.528s; Git
69.836s; projectread 76.888s; standards 59.465s; drift 25.758s;
loganalysis 33.351s; standardsexceptions 13.452s. Latest store run passed in
119.976s; daemon passed in 140.558s before the successful final caller rerun.

The lint run initially found new dead code/style issues, which were fixed.
The final command passed with zero issues:

```sh
make lint PKGS='./internal/connector ./internal/codeevidence ./internal/git ./internal/projectread ./internal/standards ./internal/store ./internal/daemon'
```

`go build ./...`, `git diff --check`, and `gofmt -l` over changed packages passed.
Markdown structural checks passed UTF-8, balanced fenced blocks and the 8 KiB
resource bound. Wording is reviewed semantically, with no exact text/hash pins.


## Required hosted-client proof unavailable

The approved worker task requires a supported existing-client walkthrough and
client/version capture. No TEST_BASE_URL or authenticated supported client/browser
access was supplied. No production service was tested. The protocol test cannot
establish hosted rendering, following source links in that client, or the complete
client walkthrough. This remains an access blocker, not an acceptance failure
inferred from a missing observation. The factory controls any subsequent step;
this report promises no retry or continued parallel work.

## Guidance observation

The existing Go overview description at internal/connector/mcp.go:370 contains
behavioral answer-order instructions. The approved architecture explicitly leaves
that existing violation outside this story. New code/standards behavioral guidance
is kept in bounded Markdown resources. No unrelated relocation was made.
