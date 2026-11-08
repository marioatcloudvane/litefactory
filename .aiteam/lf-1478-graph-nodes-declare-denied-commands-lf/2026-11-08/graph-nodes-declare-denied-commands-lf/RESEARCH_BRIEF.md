# RESEARCH_BRIEF — Graph nodes declare denied commands (`lf_deny`) — issue #1478

Status: waiting for owner answers (q1–q4). Base commit: `51bf3ddf`.

Permalink base: `https://github.com/marioatcloudvane/litefactory/blob/51bf3ddff155cf0c0f6408c1edc1d9a2dfabde75/`

## Outcome ledger

### S-01 mode=feature, size=large — new generic node attribute, two runtime enforcement paths (Claude simple, Codex needs a new delivery route), graph validation, team DOT values
(agent's call)

### O-01 Any `lf_agent` node may declare `lf_deny`; its entries are added to that node's run deny rules
Syntax is the shared deny-rule syntax: a comma-separated list of `Tool(pattern)` entries. Splitting on commas happens outside parentheses (agent's call). Go reads the attribute from whatever node is running and knows no mode, workflow, node or step name.
(owner said: issue #1478 "A generic node attribute … on any `lf_agent` node")

### O-02 Claude runs enforce node entries through the per-run settings `permissions.deny`, which also binds subagents
The per-run settings document already exists (`internal/harness/sandbox.go` `prepareSandbox`, written to `<log>.settings.json`). Node entries are appended to the deny list read from the operator settings file. Nothing is removed.
(owner said: issue #1478 "Claude: settings `permissions.deny`, which also covers subagents")

### O-03 Codex runs enforce node entries, or refuse to start and name the entry they cannot enforce
How Codex receives the rules is open: see q1. Planned mapping (agent's call, pending q1):
- `Bash(cmd args:*)` and `Bash(cmd args)` → Codex exec-policy `prefix_rule(pattern=[…], decision="forbidden")`. An exact Claude form turns into a prefix rule on Codex, so Codex denies a little more. That is safe for a deny list.
- `Bash(…)` with a `*` anywhere except a trailing `:*` → Codex cannot express it (Codex patterns have no wildcards). The launch refuses.
- `Read(path)` / `Edit(path)` → Codex permission-profile filesystem `deny` globs, if q1 = a. Otherwise the launch refuses.
(owner said: issue #1478 "If an entry cannot be enforced on a runtime, the run refuses to start and says which entry")

### O-04 A graph-level `lf_deny` default exists; how nodes combine with it is settled by q3
(owner said: issue #1478 "It can also be a graph-level default that nodes extend or override")

### O-05 Generic graph checks reject a malformed `lf_deny`
They reject an empty entry, a missing `Tool(` … `)` wrapper, an empty pattern, unbalanced parentheses, a duplicate, and a tool name outside the known set. The known set is `Bash`, `Read`, `Edit`, the tools the shared deny list uses (agent's call). This applies to node and graph level, in the shared action attribute declaration (`graphexec.AgentAttributes`) and graph validation, with no workflow-specific code.
(owner said: issue #1478 "syntax and known tool names only")

### O-06 A node without `lf_deny` (and no graph default) launches exactly as today, byte-identical settings and Codex argv
(owner said: issue #1478 "A node without it behaves as today")

### O-07 Team DOT values: which shipped team DOTs carry entries is settled by q2
(owner said: issue #1478 "Teams set their own values in their DOT files")

### O-08 Docs: one page section on `lf_deny`: syntax, merge rule, Codex mapping and refusals, and its limit (N-02)
(agent's call)

### N-01 No change to how the factory itself runs checks (Validate, stack-profile checks); those are not agent shell commands and are unaffected
(agent's call)

### N-02 `lf_deny` is a guard rail, not a security boundary
Both runtimes match the command text by prefix. `./scripts/lf-check.sh`, `bash scripts/lf-check.sh` or a script that calls it can get around an entry. See q4.
(agent's call, measured: see F-05)

### N-03 No Markdown role changes and no tests that pin Markdown wording
(owner said: issue #1478 "No tests that pin Markdown wording")

### N-04 No new tool names beyond Bash/Read/Edit in this issue (WebFetch etc. can follow later)
(agent's call)

### K-01 The shared credential/state deny list (`denyRules`) and the OS sandbox stanza stay unchanged; node entries only add
(owner said: issue #1478 "No change to the shared credential and state deny list from #1465")

### K-02 No Go code names a mode, workflow, node or step for `lf_deny`, and no deny entry is hard-coded in Go
(owner said: issue #1478 "No Go code that names a mode…")

### K-03 Codex runs without `lf_deny` keep `--ignore-rules` and ignore the operator's own `~/.codex/rules`
Loading the operator's rules could add `allow` rules that loosen the sandbox.
(agent's call)

### K-04 `make lint` is clean
(owner said: issue #1478 "`make lint` is clean")

## Findings (observed unless marked)

- **F-01 Shared deny list.** `denyRules` builds Bash/Read/Edit entries such as `Bash(git push:*)`. Path entries use `//abs` and `~/` forms. — `internal/harness/settings.go#L72`
- **F-02 Per-run Claude settings exist.** `prepareSandbox` reads `claude.settings_path`, adds the sandbox stanza and writes `<runlog>.settings.json`. The agent cannot write that file. This is the natural merge point for node entries. — `internal/harness/sandbox.go#L127`
- **F-03 Codex always runs with `--ignore-rules`.** This turns off every user and project exec-policy `.rules` file. — `pkg/execution/providers/codex/runner.go#L82`. Codex loads rules only from `rules/` folders of active config layers: the user layer (`CODEX_HOME`), a project `.codex/rules` that is trusted, or a team/system layer. There is no documented `-c` key for a rules file. So Codex needs a litefactory-owned layer the agent cannot edit. The worktree `.codex/` is agent-writable and would dirty the commit, so it is ruled out. (observed: docs, learn.chatgpt.com/docs/agent-configuration/rules)
- **F-04 Precedent for a private Codex home.** Doctor and conversation runs already use a private `CODEX_HOME` with the operator's `auth.json` symlinked in (`internal/codex/doctor.go` `linkDoctorAuth`, `internal/codex/conversation.go`). Graph runs use the operator's home today. Session resume and usage collection read rollouts from `CODEX_HOME` (`internal/codex/runner.go` `prepareRollouts`, `req.Session.Resume`). A private home must stay stable across resumes of the same session. (inferred risk)
- **F-05 Codex prefix matching, measured** with `codex execpolicy check` (codex-cli 0.160.0), rule `prefix_rule(pattern=["scripts/lf-check.sh"], decision="forbidden")` plus `["make","check"]`:
  - `scripts/lf-check.sh` → forbidden; `scripts/lf-check.sh --all` → forbidden; `make check` → forbidden
  - `./scripts/lf-check.sh` → no match; `bash -lc 'make check && ls'` → no match in the checker. Codex docs say the runtime splits simple `&&`/`;`/`|` chains, but not scripts with redirection, substitution or variables.

  Claude `Bash(x:*)` is also prefix matching (inferred from Claude docs; not measured here).
- **F-06 Codex file denies.** Codex permission profiles support filesystem `deny` globs (`"**/*.env"="deny"` under `:workspace_roots`), and deny beats write. This is a beta feature. litefactory already selects a named profile for read-only reviewers via `default_permissions` (`internal/codex/runner.go` args). Writable graph runs use `--sandbox workspace-write`, not a profile.
- **F-07 Node attributes reach the run** through `applyNodeAccess` (`internal/turn/workflow_access.go`). There are five graph launch sites that build `agent.Request.Graph`: `turn/research_workflow.go`, `research_agent.go`, `plan_graph.go`, `bug_graph_runtime.go`, `validate_runtime.go`. The closest generic precedent is `lf_budget` from #1468 (`graphexec.AgentBudget`, `AgentAttributes`).
- **F-08 Graph validation.** `pkg/execution/graph/validate.go` `validateNode` checks declared attributes against closed value lists. `lf_deny` needs a free-form validator hook. `Graph.Attrs` holds graph-level attributes (`pkg/execution/graph/graph.go`).
- **F-09 Team DOTs are generated.** `teams/<stack>/v1.0.0/**` comes from `teams/source/base-team` plus a stack overlay via `go run ./cmd/teamrelease`. Spike/implement DOTs come from base-team only, so a per-stack value needs an overlay mechanism or variables (`teams/source/python-fastapi/inherit.json`). Shipped stacks have no `scripts/lf-check.sh`: their full checks are factory-run `python3 -c …` argv in `stack-profile.json`. `scripts/lf-check.sh`, `scripts/ci-test.sh` and the browser check are cockpit's own scripts. Operators make editable local teams with `team clone` (`docs/cli/teams.md`).

## Impact analysis

- **Touched:** `pkg/execution/graph` (validation hook), `internal/graphexec` (attribute declaration, parse/merge helper), `internal/turn` (carry entries onto the request at the five launch sites), `internal/agent` (new request field), `internal/harness/sandbox.go` (merge into per-run deny), `internal/codex` + `pkg/execution/providers/codex` (rules/profile delivery, refusal), docs, team sources if q2 ≠ a.
- **Depends on it:** every graph agent launch, for Claude and Codex. Judge, advisory, doctor and conversation runs are not graph nodes and stay unaffected (observed: they build their own argv).
- **Superseded:** none. The Markdown-only request (separate PR) stays.
- **Data and compatibility:** no DB migration. Existing installed teams have no `lf_deny`, so they behave as today. A team release that adds entries is a new team version.

## Open questions (sent to owner)

- q1 technical — the Codex delivery route (F-03, F-04, F-06)
- q2 business — which shipped teams carry entries (F-09)
- q3 technical — how a graph default and a node value combine
- q4 business — whether prefix matching is enough (F-05)
