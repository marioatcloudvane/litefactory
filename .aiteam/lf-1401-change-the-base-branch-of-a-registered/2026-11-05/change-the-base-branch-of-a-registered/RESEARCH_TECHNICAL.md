# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

The CLI has `repo add` and `repo list` only. `repo add` on an already registered path updates the row and keeps earlier base, worktree and prefix values when flags are omitted. So `repo add <path> --base stage` already changes the stored base, with no remote check and no open-work check.

Evidence:

- `internal/cli/cmd_repo.go:47`
- `internal/factory/localprojects/registration.go:55`

## f2

Base validation is name-shape only (regex), and resolution never touches the network by design. A remote-existence check is therefore new behaviour. The git layer already runs `ls-remote --refs origin <ref>` elsewhere, so a primitive exists, but `repos.Resolve` and the read-only Clone type expose none for this.

Evidence:

- `internal/repos/registration.go:205`
- `internal/git/retirement_remove.go:81`
- `internal/git/integrate.go:16`

## f3

The base branch lives in two places: the `repos.base_branch` column, and the code-role project connection version (`base_branch`, `clone_path`, ...). Re-registration calls ImportLegacyConnections, which only inserts a connection when none exists and skips otherwise. So `repo add --base` likely leaves the connection's base stale. The assumption is not tested against the daemon.

Evidence:

- `internal/store/project_connections.go:339`
- `internal/projectconnection/import.go:333`
- `internal/factory/localprojects/connections.go:55`

## f4

Execution snapshots pin a connection version per session, and the run target is built from that version, not from current settings. So sessions already started keep their old base target (matching the 'open PRs keep their target' expectation), and new sessions pick up whichever connection version is current.

Evidence:

- `internal/store/execution_snapshots.go:35`
- `internal/store/execution_snapshots.go:51`

## f5

Many readers use the repos row's base directly: worktree creation, delivery target, sync, harvest, retirement and project read. These are in turn/prepare.go, turn/delivery_activity.go, turn/validate_runtime.go, turn/bug_graph_runtime.go, daemon/retirement_execute.go and projectread. Retirement compares the base across a fresh read and aborts if it changed. A mid-run change to the row would therefore affect open work, which is why open work must be checked.

Evidence:

- `internal/turn/prepare.go:786`
- `internal/turn/delivery_activity.go:168`
- `internal/daemon/retirement_execute.go:219`

## f6

Open-work signals exist in the schema: `runs.status='running'`, `graph_instances.status='running'` and `graph_node_attempts.status IN ('pending','running')`. No store helper for 'repo has open work' was found by name. Repo to graph instance mapping was not traced in this pass.

Evidence:

- `internal/store/migrations/0001_init.sql:84`
- `internal/store/migrations/0048_graph_execution_foundations.sql:26`

## f7

`repo list` already prints the BASE column from the repos row, so the list requirement is met once the row is updated. There is no unregister command anywhere in the CLI.

Evidence:

- `internal/cli/cmd_repo.go:167`

## f8

Impact. Touched: the cli repo commands, the localprojects administration, the store (a repo base update plus a new connection version, and an open-work query), and the git package (remote branch check), with tests and the CLI docs. Depends on it: delivery, worktree, sync and retirement paths via repo.BaseBranch; config `default_base_branch` is unaffected. Superseded: nothing; `repo add --base` keeps working. Data: no migration is needed, since both columns exist. Existing main-based repos are unaffected as long as the command only runs on explicit request.

Evidence:

- `internal/factory/localprojects/registration.go:104`
