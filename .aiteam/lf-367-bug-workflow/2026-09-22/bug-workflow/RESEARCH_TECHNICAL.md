# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Owner request: add a lightweight workflow for bugs on the existing graph engine. Analysis covers root cause, impact and solution and asks where the bug occurs when ambiguous; repair changes code, updates unit tests and reruns the suite. The subject is the workflow itself, so no concrete failing behavior exists to reproduce.

Evidence:

- `.aiteam/lf-367-bug-workflow/2026-09-22/bug-workflow/external-evidence.md:3`

## f2

The mode vocabulary is closed to five values (research, plan, implement, validate, spike); there is no Bug value. Execute admits Plan and Research graph routes explicitly.

Evidence:

- `internal/agent/mode.go:11`
- `internal/turn/execute.go:631`

## f3

lf_agent, lf_if, lf_loop, lf_comment and lf_ask are generic registered actions valid for any admitted mode. WorkflowPolicy supplies domain-specific decode, result, artifact and publication contracts. Research shows the reuse pattern: one specific activity (lf_research_brief) plus generic nodes, per-node lf_limit, lf_source and durable waits.

Evidence:

- `internal/graphexec/registry.go:37`
- `internal/turn/workflow_policy.go:13`
- `internal/teamsource/research/research.dot:18`

## f4

The DOT profile rejects subgraphs and ports. Composition tests use a fake future activity; no production child-graph invocation exists. The owner chose a separately executed child test graph (q4 b), so this is new engine capability.

Evidence:

- `internal/graphexec/dot.go:68`
- `internal/turn/graph_composition_test.go:14`

## f5

#319 'Wire graph-based execution into Implement mode' was still open when checked on 2026-09-24. Its lf_implement_* names were proposals only.

Evidence:

- `.aiteam/lf-367-bug-workflow/2026-09-22/bug-workflow/external-evidence.md:10`

## f7

Lifecycle constraints separate engineering completion, acceptance and delivery. CONC-11 forbids automatic mode chaining and requires PR/current-head/main approval and confirmed integration before closure. CONC-23 grants default question permission only to Research and Plan, so a Bug analyst that asks questions needs an explicit permission.

Evidence:

- `.aiteam/APP_CONSTRAINTS.md:207`
- `.aiteam/APP_CONSTRAINTS.md:223`
- `internal/turn/ask.go:12`

## f8

Local baseline: make test runs go test with the race detector; make check runs formatting, vet, pinned lint and race tests. Not executed in this pass.

Evidence:

- `Makefile:21`

## f11

CHALLENGE (label routing). The bug label is already common in this repository and is often combined with a Mode label: daemon tests treat it as an incidental label next to Mode: Plan/Research/Implement/Validate/Spike, and live open bug issues carry Mode labels. Mode resolution reads only Mode: labels; with none present the daemon calls an automatic classifier. A rule that bug takes precedence over Mode labels would re-route existing bug-labelled issues and override deliberate owner mode choices. Bug-as-label-only cannot distinguish 'run the bug workflow' from 'this issue is a bug'.

Evidence:

- `internal/daemon/mode.go:21`
- `internal/daemon/mode.go:189`
- `internal/daemon/mode_internal_test.go:17`
- `internal/daemon/mode_retention_test.go:78`

## f12

CHALLENGE (namespace and mode coupling). validActionName only accepts lf_<mode>_<capability> when the mode is one of the five valid modes. Admitting lf_bug_* therefore needs either a new Mode value (whose Label() would be 'Mode: Bug', ParseModeLabel, retention and classifier prompt all change, contradicting the no-Mode-label answer) or a new workflow-identity concept in the registry. The saved decision d18 states the intent but not this cost.

Evidence:

- `internal/graphexec/registry.go:37`
- `internal/agent/mode.go:35`
- `internal/daemon/mode.go:83`

## f13

CHALLENGE (test execution is new). Non-test product code that starts external processes is limited to agent runners, git, launchd, keychain and identity helpers; I found no factory-run project test or check runner, and Validate mode is agent-run and read-only. A search that finds nothing is not proof of absence, but lf_bug_checks and lf_bug_recipe would most likely be the first factory-executed project commands: command allowlist, timeouts, environment, log storage and cancellation are new design, not reuse.

Evidence:

- `internal/harness/runner.go`
- `internal/git/exec.go`
- `internal/agent/mode.go:19`

## f14

CHALLENGE (delivery reuse is partial). GitHub-level MergeDeliveryPR (atomic expected-head guard) and CloseDeliveredParent (numeric identity check) exist. The turn and store layers are feature-shaped: DeliveryProposal requires a parent task, ParentID, generation and Target 'main', and the gateway interface is named for delivered parents. lf_bug_pr, lf_bug_merge and lf_bug_close can reuse the GitHub client calls but need their own or generalized proposal bookkeeping.

Evidence:

- `internal/github/delivery.go:130`
- `internal/github/delivery.go:153`
- `internal/store/delivery.go:12`
- `internal/turn/feature_delivery.go:24`

## f15

CHALLENGE (budgets). Graph limits are per node (lf_limit, lf_loop with lf_source). The saved scope requires one repair budget shared by test failures and PR feedback across two loops, persistent across restarts and unresettable by replies, with eight analyst invocations as default. Whether lf_loop can share a budget across nodes was not established; the persistence of per-node limits across replies is documented for Research.

Evidence:

- `internal/teamsource/research/research.dot:18`

## f16

CHALLENGE (lightweight). The saved scope adds two agents, ten activities, a new routing path, a Bug result policy and a child-graph capability. Research is delivered with one specific activity. Several proposed activities (prepare, record, handoff) are bookkeeping that could be folded into their neighbors; the count is a proposal, not an established need.

Evidence:

- `internal/teamsource/research/research.dot:21`
- `.aiteam/lf-367-bug-workflow/2026-09-22/bug-workflow/BUG_SCOPE.md`

## f17

CHALLENGE (independence confirmed, MCP rule). Product code contains no lf_implement_* or lf_bug_* activity; only tests use such names, so #367 does not collide with delivered code. Open bug #377 records that graph Plan bypasses the shared MCP result service via NodeProtocol and asks that every workflow policy use it. A Bug policy must use the shared preflight/submit/receipt path from the start, consistent with d11.

Evidence:

- `internal/graphexec/graphexec_test.go`
- `internal/turn/graph_composition_test.go`

## f18

RESOLVES f15. Graph limits count the maximum dispatches of a node per instance, across every entry edge, so one attempt node entered from test failure and from PR feedback shares one repair counter. The DOT profile names its identity with a mode attribute only, so a workflow identity needs a new profile attribute.

Evidence:

- `internal/graphexec/doc.go:9`
- `internal/graphexec/doc.go:13`
