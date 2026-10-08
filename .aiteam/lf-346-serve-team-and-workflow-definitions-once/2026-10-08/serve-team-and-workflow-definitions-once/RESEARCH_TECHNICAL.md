# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

The daemon resolves the repository binding under the catalogue lock, selects the installed workflow, checks fetched release digest and requested workflow availability, captures its owned graph closure and team.config, and records source/version/config provenance. An absent binding returns nil.

Evidence:

- `internal/daemon/workflow_source.go:23`
- `internal/daemon/workflow_source.go:34`
- `internal/config/paths.go:137`

## f2

capturedWorkflow restores a retained admission first. For fresh admission it invokes WorkflowSource, but nil selection falls through to the supplied builtin capture and sets builtin provenance. Research, Plan, Implement, Validate, Spike and Bug call this shared boundary.

Evidence:

- `internal/turn/workflow_capture.go:22`
- `internal/turn/workflow_capture.go:53`
- `internal/turn/research_admission.go:80`
- `internal/turn/plan_graph.go:138`
- `internal/turn/spike_admission.go:59`
- `internal/turn/bug_graph_runtime.go:64`

## f3

Restoration validates saved resource paths and limits, reconstructs capture from retained files, checks content/topology/identity and retains provenance without reopening the installed package. Bundle verification separately refuses edited or missing saved bytes.

Evidence:

- `internal/turn/workflow_capture.go:71`
- `internal/turn/delivery.go:90`

## f4

Six production go:embed directives cover research, plan, implement, validate, spike and bug. Generic CaptureWorkflowIdentity and rendering/capture structures are still used by installed-package selection and retained restoration, so the whole teamsource package cannot simply be deleted.

Evidence:

- `internal/teamsource/research.go:22`
- `internal/teamsource/research.go:127`
- `internal/teamsource/plan.go:16`
- `internal/teamsource/implement.go:17`
- `internal/teamsource/validate.go:17`
- `internal/teamsource/spike.go:16`
- `internal/teamsource/bug.go:7`
- `internal/turn/team_validation.go:51`

## f5

NewValidateRuntime performs CaptureValidate at startup before repository-specific run binding. That embedded startup check must be removed or replaced. captureImplement and captureSpike helpers also call embedded captures; observed references to these helpers are in tests.

Evidence:

- `internal/turn/validate_runtime.go:34`
- `internal/turn/implement_graph.go:211`
- `internal/turn/implement_graph_test.go:90`
- `internal/turn/spike_admission.go:106`
- `internal/turn/spike_fix_test.go:799`

## f6

ValidateBugParent reads embedded bug.dot and compares the parsed digest against a builtin identity and exact topology. It is called by the embedded CaptureBugParent wrapper; installed workflow validation instead uses generic CaptureWorkflowIdentity. Removing the wrapper requires separating any factory-owned invariant worth retaining from editable DOT bytes.

Evidence:

- `internal/teamsource/bug_parent.go:13`
- `internal/teamsource/bug_parent.go:31`
- `internal/turn/team_validation.go:51`

## f7

prepare constructs delivery only when a graph definition is supplied. With Delivery nil, Claude omits bundle setting-source/plugin/bootstrap selection and Codex omits delivered role/main instructions, strict config and ambient-skill overrides. Codex ignore-user-config also depends on configured userConfig or delivered bundle. The observed per-worktree reading is host discovery rather than a Go role-file loader; suppressing or refusing this path can be handled without proving the old engine must be deleted.

Evidence:

- `internal/turn/prepare.go:224`
- `internal/harness/runner.go:674`
- `internal/codex/runner.go:291`
- `internal/codex/runner.go:425`
- `internal/codex/runner.go:445`
- `internal/codex/skills.go:15`
- `internal/turn/execute.go:104`

## f8

Catalogue checks validate listed SHA256 hashes, unlisted release files, rejection of standard naming, equality with internal/teamsource bytes, file-source install integrity and reinstall behavior. Equality against teamsource becomes obsolete; installability/hash/completeness checks remain relevant. team.config maps all six workflows. teams/source was absent in the inspected checkout.

Evidence:

- `internal/teampackage/shipped_catalogue_test.go:17`
- `internal/teampackage/shipped_catalogue_test.go:48`
- `internal/teampackage/shipped_catalogue_test.go:69`
- `teams/go-development/v1/team.config:1`

## f9

Fixtures and contract checks span teamsource, turn and daemon: embedded asset variables, builtin capture APIs and shipped-role graph checks are directly consumed. Config's agent tier test reads repository .claude/agents and compares the tier roster, so deleting repository copies also requires changing that test's source. Preserve bounds, defaults, closure, delivery and retained-identity coverage through test-owned fixtures; retain shipped-team contract checks against the surviving release source.

Evidence:

- `internal/teamsource/research_test.go:181`
- `internal/teamsource/plan_set_role_test.go:14`
- `internal/teamsource/research_response_contract_test.go:16`
- `internal/teamsource/shipped_roles_test.go:120`
- `internal/turn/workflow_capture_test.go:22`
- `internal/daemon/workflow_source_internal_test.go:48`
- `internal/config/agent_tiers_test.go:24`

## f10

AGENTS.md combines obsolete local-role routing with build/test, daemon-state, SQL/package ownership and completion guidance; CLAUDE.md repeats routing/completion. APP_CONSTRAINTS remains the normative repository rule source. Existing team fixer guidance already covers package checks and factory-owned result submission but still refers to AGENTS/CLAUDE as possible command guidance. Docs still describe or link embedded workflow files; these references need updating with the deletion.

Evidence:

- `AGENTS.md:1`
- `CLAUDE.md:1`
- `.aiteam/APP_CONSTRAINTS.md:1`
- `teams/go-development/v1/bug/bug-fixer.md:31`
- `teams/go-development/v1/bug/skills/bug-repair-result/result.md:9`
- `docs/workflow/research-graph.md:7`
- `docs/workflow/plan-graph.md:11`
- `docs/workflow/validate.md:5`
- `docs/workflow/spike.md:36`
