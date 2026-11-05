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

Coding Agent labels are parsed by the prefix `Coding Agent: ` (case-insensitive detection, strict acceptance) in the daemon. Anything malformed or unresolved is refused. This is the GitHub-style flat name that the Linear reader must also accept as a group-and-child pair.

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

A repo-wide search found no `--bug-label` or BugLabel code. #1425 (Linear bug filing) is open; its scope adds one optional Linear connection setting naming an existing bug label, set like the mode labels. Its N-08 says filing never creates a label. That rule concerns filing only, so a separate owner-run setup command does not conflict with it.

Evidence:

- `internal/cli/cmd_repo_connections.go:35`

## f8

Impact. Touched: internal/github (label list and create), internal/linear (label query with parent, create with parentId, group-aware matching), internal/store (a connection version change for coding-agent and bug maps, plus migration), internal/factory/localprojects and contracts (request and view), internal/cli (new command, check output, hints), internal/projectconnection and localadmin/health (missing-label reporting), internal/daemon agent inheritance (chosen in A-04), and tests. Depends on it: the daemon's agent and mode resolution, bug filing (#1424, #1425), and existing cockpit (MAR) connections whose flat mode maps must keep working. Superseded: hand-written ops scripts and manual `--mode-labels` editing, which stay supported. Data: ConnectionVersion is immutable and versioned, so new map fields need a new revision per connection and must be optional for old ones. Team roles and skills: none found that name label setup (searched the plugin skills shipped with this run).

_No source reference was recorded for this finding._

## f9

`repo add` documents that nothing is fetched over the network. Running label setup inside it would break that promise, so the hook for 'when a project is added' must be a hint or an opt-in step.

Evidence:

- `internal/cli/cmd_repo.go:55`

## f10

A Linear connection cannot be saved without a valid mode-label map: validation requires 1-32 entries. A fresh Linear project therefore has no connection for setup to read team and credentials from, unless that rule is relaxed or setup creates the connection itself.

Evidence:

- `internal/store/linear_connections.go:56`
- `internal/store/linear_connections.go:33`

## f11

The daemon copies a parent's Coding Agent label onto a story without one. On Linear this write fails today because the client writes only mapped mode labels; the daemon logs a warning and still runs the story with the parent's agent.

Evidence:

- `internal/daemon/agent_inherit.go:41`
- `internal/linear/tickets.go:620`

## f12

Required Linear modes are research, plan, implement, validate and spike, matching the owner's Mode group children.

Evidence:

- `internal/linear/tickets.go:684`

## f13

External contract (from third-party docs, not yet verified live). Linear GraphQL: issueLabels can return parent, isGroup and team (null team means workspace label); issueLabelCreate takes name, color, teamId, parentId and isGroup. GitHub REST: list repository labels (paginated, up to 100 per page) and create a label with name, color and description; creating needs write access to the repository's issues. Whether isGroup works on issueLabelCreate must be confirmed against Linear's own schema or a live team. Sources: https://docs.rs/linear-api/0.1.0/linear_api/labels/struct.LabelCreateInput.html , https://www.withone.ai/knowledge/linear/conn_mod_def%3A%3AGJ4vKHpIvP0%3A%3ASYDSvBDjRQ-RUcVKnSu7Lg/md

_No source reference was recorded for this finding._
