# Research brief — #1478 named deny groups

Status: incomplete; owner changed the interface and requested a runtime comparison before answering q1–q4; q5 clarifies agent calls. Inspected commit: 9957dd279d5336448c407194f02e33493326dd82. No implementation, builds or test suites run.

## Outcome ledger

### S-01 mode=feature, size=large — settings groups, graph references and enforcement across two runtimes
(agent's call)

### O-01 Graph and agent-node lf_deny values contain only group names, and settings hold each group's deny list
This supersedes inline DOT rules and the original DOT-only configuration requirement; Go remains independent of workflow and step names. (owner said: comment 6763254390)

### O-02 Claude appends resolved group entries to the existing per-run permissions.deny list
The rules must also bind subagents; existing shared restrictions remain intact. (owner said: issue #1478)

### O-03 Codex enforces every resolved entry or refuses launch with the group, entry and reason
A private factory-owned configuration home is recommended, pending q1; command policy, filesystem permissions and subagent controls require distinct translations. The previous full-parity claim is withdrawn: supported mappings require evidence from the actual runtime, not just its policy checker. (owner said: issue #1478)

### O-04 Graph defaults and node groups combine according to the still-open q3
Recommend additive union with no node escape from graph defaults. (agent's call)

### O-05 Graph checks validate group-reference syntax and configured admission resolves groups and validates entries
Reject raw rules in DOT, malformed references, unknown groups, malformed entries and unknown tool names; syntax validity is separate from runtime support. Static team checks without operator settings cannot check catalogue membership, so configured admission must check it before launch. (agent's call)

### O-06 Nodes with no selected groups retain current permission behavior
No new private Codex home or deny-related argument changes apply to those runs. (owner said: issue #1478)

### O-07 Which shipped team graphs select groups remains open in q2
Recommend no invented stack-specific commands; cockpit defines its own group and selects it in its local team. (agent's call)

### O-08 Documentation explains group syntax, resolution, runtime differences and refusal behavior
Include a copyable example and native matching limitations. (agent's call)

### O-09 Agent-call entries are included once q5 settles what agent calls means
Recommend native CLI subagent restrictions; shell-launched agent CLIs are already Bash entries, while factory workflow routing is separate. (agent's call)

### O-10 Groups are optional operator settings activated only by graph references
Propose settings.denyrules, an empty catalogue by default, no nested groups and no implicit script shorthand; see [deny-group contract](DENY_GROUP_CONTRACT.md) for the proposed interface. (agent's call)

### N-01 Factory-run validation checks are unaffected
These are not the agent's shell commands. (agent's call)

### N-02 Command matching is a guard rail, not a promise to catch every indirect execution
Alternate paths and wrappers need explicit coverage; current Claude docs describe wrapper parsing, so the earlier claim that bash wrappers always bypass both runtimes is withdrawn. q4 remains open. (agent's call)

### N-03 No Markdown role changes or wording-pinning tests
(owner said: issue #1478)

### N-04 The earlier Bash/Read/Edit-only exclusion is superseded by O-09
(owner said: comment 6763254390 explicitly includes agent calls)

### K-01 Shared credential and state restrictions remain intact and selected groups only add restrictions
(owner said: issue #1478)

### K-02 No mode, workflow, step names or team-specific deny entries are hard-coded in Go
(owner said: issue #1478)

### K-03 Runs without selected groups retain the existing rule-loading behavior
Do not import unrelated personal or project Codex allow rules when enabling factory restrictions. (agent's call)

### K-04 make lint must pass in later implementation validation
(owner said: issue #1478)

### K-05 Codex authentication, session resume, usage collection and existing sandbox limits remain functional
A private home must be stable for its session, protected against agent edits, and receive the correct restrictions on each attempt. (agent's call)

### A-01 Owner changes the interface and requests a comparison before answering
> some remarks before i answer questions: lf_deny should "only" have groups; we would define a group in the settings under a new setting like this:
>
> `denyrules: 
>     rule_xx:
>         deny: 
>             <list of scripts, bash, agent calls, ...>
> `
> where the list is what gets denied and thus forwarded to the cli's ... 
>
> also, list me how this would work in codex vs. claude!

(owner said: comment 6763254390; q1–q4 remain unanswered)

## Current findings and corrections

Observed at the inspected commit unless labelled otherwise:

- F-10: Config is strictly decoded with KnownFields(true); no deny-group catalogue exists. Optional groups must join the operator configuration schema and validation. Sources: internal/config/config.go:41,578 and docs/configuration/config-yaml.md.
- F-11: Claude documents Agent and Agent(name) denies; Codex already uses features.multi_agent=false in its adapter, but an equivalent selective named-agent ban has not been established. Sources: internal/codex/runner.go:447 and https://code.claude.com/docs/en/permissions#agent-subagents.
- F-12: Current Codex rules docs describe forbidden prefix rules and shell-wrapper parsing, but describe their purpose in terms of execution outside the sandbox. The retained checker measurement does not establish every live shell/tool path. Launch support requires live enforcement evidence, otherwise refuse. Sources: https://developers.openai.com/codex/rules and https://code.claude.com/docs/en/permissions.
- F-13: Team source layout changed since the earlier pass: commit b5eb77e1 deleted generated team releases and cmd/teamrelease. F-09 below is historical, not the current implementation path. Sources: git history and teams/source.
- F-14: Related #1465 and #1468 appear in repository merge history; issue-summary lookups failed to return summaries, so no refreshed remote status is claimed. Retained request IDs: 33bd85027b1d8149531a1a4c01ce6282 and 0853bef94342e0154153fc3152d60ec3.
- F-15: Earlier #1268 Research retains neutral graph/provider packages and internal factory policy ownership. Apply plan-go-guidance to preserve these boundaries. Sources: .aiteam/lf-1268-extract-reusable-graph-and-agent/2026-10-31/extract-reusable-graph-and-agent/RESEARCH_BRIEF.md, pkg/execution/graph/validate.go and internal/graphexec/registry.go.

## Retained prior findings

These observations were recorded at 51bf3ddff155cf0c0f6408c1edc1d9a2dfabde75; current corrections above take precedence. The CLI measurements were not rerun. In particular, file denies do not prove identical Read/Edit semantics, and the policy checker does not prove live enforcement.

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

- Touched: operator config schema/validation and docs; generic graph attributes and configured group resolution; agent request boundary; Claude settings preparation; Codex rule/profile delivery; team sources only if q2 selects them.
- Depends on it: every graph agent launch and its descendants; authentication, resume and usage collection depend on Codex home handling. Non-graph doctor, judge and conversation behavior remains unchanged.
- Superseded: previous inline-DOT proposal, not a shipped implementation; generated teamrelease paths in the earlier findings are obsolete.
- Data and compatibility: optional settings catalogue with no database migration expected; existing teams without references retain behavior. A team that adds a reference requires a matching operator group. No automatic mutation of installed teams or shared settings.
- Permission boundary: preserve current sandbox and shared restrictions; freeze resolved rules for a running attempt and resolve current configured rules before each new attempt, including resume. Unsupported semantics must fail before agent launch.

## Success evidence required from later work

Show malformed reference/entry rejection, unknown-group refusal, shared-deny preservation, no-group compatibility, supported command failures in both live runtimes, unsupported-entry refusal, descendant enforcement and preserved resume/usage handling. File mappings must preserve read/write intent or refuse, not silently broaden behavior and claim parity. Run make lint during implementation/validation. These are acceptance obligations, not executed checks.

## Open owner choices

- q1 technical: private Codex delivery route versus Claude-only initial support.
- q2 business: local team groups versus shipped stack groups.
- q3 technical: additive defaults versus node replacement.
- q4 business: explicit native matching versus automatic common command variants.
- q5 business: native CLI subagent calls versus shell CLI calls versus factory routing.

The named-group interface is settled. None of q1–q4 was answered by the owner; do not infer approval from the request for an explanation.
