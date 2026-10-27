# Code and standards example check

The guide's three conversations are illustrative, not live installation results.
The main-path integration check exercised the public browser consent, OAuth and
MCP interfaces using a local HTTP handler, real temporary Git repositories and a
real verified published standards book. Only the upstream identity verifier was
replaced. No production service or external database was contacted.

Executed command:

```sh
go test -run '^TestOwnerCodeStandardsConsentToEvidence$' -timeout=5m -v ./internal/codeevidence
```

Result: PASS. Final focused run completed in 1.941 seconds. This is focused
engineering verification, not the package's final complete race run or acceptance.

Observed fixture identifiers:

- Task ref: `refs/heads/review-task`.
- Original task commit: `06fda8dd78c8ce8a43a6362812657e8054e8e263`.
- Base commit: `87252f5c6046851f9758993dd7c639eda392a080`.
- Published book revision: `1`.
- Published book digest: `7d8481e38176b35e06f2f4f59463a4fa5313ed5735a6745983cc96ba9218952b`.

The real tool output agreed with the examples:

1. `repo_read` returned committed `limits.go` at line 3 with `Limit = 3` and its exact `#L3` source link, alongside
   the actual task ref and full commit. The base read returned `Limit = 1`.
   The dirty working file's secret marker was absent. After a local fixture
   commit changed the limit to 9, the retained view still returned the original
   commit and value; a new view returned the new commit and value.
2. `standards_read` returned `review-limit`, with the pinned book digest.
   Its first page supplied a protected token; a token-only follow-up reached
   `test-evidence` and completed the two-rule book. Same-view role counts gave
   `story-worker` selected review-category count 1 and whole-book count 2.
   The overlapping `validate-architecture` role had 1 and 1. The known empty
   category gave selected count 0 and whole-book count 2 for `story-worker`.
   These are the guide's count facts; their relationship to the positive
   declaration remains a labeled interpretation, not compliance proof.
3. A traversal path received a JSON-RPC refusal. After local revocation, a
   fresh committed read received HTTP 401. Repository working-tree status was
   unchanged by discussion reads.

The same path obtained all six installed code/standards tools after explicit
owner consent to both scopes on a task whose issue and code repositories differ.
It also read committed search, history and a base-to-task diff. Each successful
response was checked against the complete 128 KiB ceiling and the 1,000 Unicode
code-point summary ceiling.

`make lint PKGS=./internal/codeevidence` also passed with zero issues. Its default
linter cache emitted sandbox write warnings; no lint finding remained.

Markdown structural checks validated UTF-8, balanced fenced blocks and the
per-resource 8 KiB guidance ceiling. No wording or byte pins were introduced.

# Limitations

The owner has deferred the hosted-client walkthrough to a manual check they will
perform afterward, including their production client where applicable. No worker
production test was run, and no completed client walkthrough or client/version
capture is claimed here. The protocol integration test does not establish
hosted-client rendering or source-link navigation. Those observations remain
pending the owner's manual check; the engineering results above remain separate
from acceptance, which belongs to Validate.
