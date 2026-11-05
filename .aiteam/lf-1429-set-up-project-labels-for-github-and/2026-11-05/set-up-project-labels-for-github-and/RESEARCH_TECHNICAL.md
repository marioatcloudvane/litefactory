# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

GitHub: the only label write is AddIssueLabel, which adds one exact label to an issue. There is no label list or create call, so GitHub auto-creates unknown labels grey and without a description.

Evidence:

- `internal/github/issues.go:246`

## f2

Linear: mode labels are found only by ID through ModeLabelsJSON, a JSON map stored on the immutable connection version. It is validated as 1-32 non-empty entries. There is no field for coding-agent labels or a bug label.

Evidence:

- `internal/store/linear_connections.go:12`
- `internal/store/linear_connections.go:31`

## f3

Linear issue reading fetches labels as only `id name` (no parent group). A label is canonicalised to a mode only when its ID is in the mode map. A grouped child therefore shows as its bare child name, which is why `Coding Agent › Codex` becomes `Codex`.

Evidence:

- `internal/linear/tickets.go:23`
- `internal/linear/tickets.go:110`

## f4

Coding Agent labels are parsed by the prefix `Coding Agent: ` (case-insensitive) in the daemon. Anything malformed or unresolved is refused. This is the GitHub-style flat name that the Linear reader must also accept as a group-and-child pair.

Evidence:

- `internal/daemon/agent.go:21`
- `internal/daemon/tick.go:3278`

## f5

Mode label adding and removing on Linear works from mapped IDs only. Missing mappings are reported with advice to edit `--mode-labels` by hand, in the health check and the connection check.

Evidence:

- `internal/linear/tickets.go:620`
- `internal/factory/localadmin/health.go:93`
- `internal/projectconnection/linear.go:87`

## f6

CLI today: `repo connection set` takes `--mode-labels` as raw JSON of label IDs. `repo connection check` prints status, capabilities and a remedy. There is no `repo labels` command, and `repo add` does not touch labels.

Evidence:

- `internal/cli/cmd_repo_connections.go:35`
- `internal/cli/cmd_repo_connections.go:84`
- `internal/cli/cmd_repo.go:51`

## f7

A repo-wide search found no `--bug-label` or BugLabel code. #1425 (Linear bug filing) is open and its story describes a bug-label setting the owner picks. This issue says it 'fills the same setting #1425 introduces', so ordering and ownership of that field is an open dependency.

Evidence:

- `internal/cli/cmd_repo_connections.go:35`

## f8

Impact. Touched: internal/github (label list and create), internal/linear (label query with parent, create with parentId, group-aware matching), internal/store (a connection version change for coding-agent and bug maps, plus migration), internal/factory/localprojects and contracts (request and view), internal/cli (new command, check output), internal/projectconnection and localadmin/health (missing-label reporting), and tests. Depends on it: the daemon's agent and mode resolution, bug filing (#1424, #1425), and existing cockpit (MAR) connections whose flat mode maps must keep working. Superseded: hand-written ops scripts and manual `--mode-labels` editing, which stay supported. Data: ConnectionVersion is immutable and versioned, so new map fields need a new revision per connection and must be optional for old ones. Not examined: the exact migration mechanics and the GitHub rate-limit and permission behaviour for label creation.

_No source reference was recorded for this finding._
