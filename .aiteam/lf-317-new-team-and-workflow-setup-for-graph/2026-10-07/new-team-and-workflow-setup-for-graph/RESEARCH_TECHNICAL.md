# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Built-in mode capture wrappers use go:embed and fs.Sub, then feed the generic CaptureWorkflow/CaptureWorkflowIdentity reader. That generic reader already takes fs.FS, graph path, team identity and an admitted registry; it checks workflow identity, graph actions, role metadata, effective task/tier, skill dependencies and resources. This is the principal reuse seam, not an existing end-user external-team selector.

Evidence:

- `internal/teamsource/research.go:26`
- `internal/teamsource/research.go:117`
- `internal/teamsource/implement.go:12`

## f2

Capture restricts resource paths, regular file reads to 1 MiB and aggregate snapshots to 16 MiB; hashes resource bytes and returns owned copies. Immutable graph definitions and saved launch files already exist. An installer still needs confined extraction/download validation and must not substitute mutable installed files for pinned running instances.

Evidence:

- `internal/teamsource/research.go:71`
- `internal/teamsource/snapshot.go:3`
- `internal/store/graph_definitions.go:13`
- `internal/turn/research_workflow.go:337`

## f3

Current config explicitly refuses teams and settings.team_config as retired keys. CLI tests require old team subcommands to be unknown and omitted from help. Migration 0098 drops repository team-selection columns. New configuration, installation paths and repository bindings require new supported surfaces; old data cannot be assumed available.

Evidence:

- `internal/config/config.go:725`
- `internal/cli/team_retirement_unit_test.go:8`
- `internal/store/migrations/0098_remove_repository_team.sql:1`
- `internal/config/paths.go:27`

## f4

Production admission/preparation callers still capture embedded packages directly for Research, Plan, Implement, Spike and Bug. Advisory unwiring sketch: introduce a repository-aware package resolver at these capture boundaries; add source/catalogue config, Paths-owned version directories and store-owned bindings; validate selected package against factory action registries before admission. Implement catalogue/install/inspect/update/remove and link/unlink over that resolver. Keep provider delivery and result authorization factory-owned.

Evidence:

- `internal/turn/research_admission.go:80`
- `internal/turn/plan_graph.go:138`
- `internal/turn/implement_graph.go:211`
- `internal/turn/spike_admission.go:105`
- `internal/turn/bug_graph_runtime.go:27`
- `internal/turn/workflow_policy.go:19`

## f5

Workflow identities are closed to research, plan, implement, validate, spike and bug. The registered graph policy list includes Research, Plan, Bug, Spike and Implement, without Validate. Registry validation rejects unknown actions and actions owned by another workflow; ExecuteWorkflow rejects mismatched profile, policy and requested mode. A cross-mode Fasttrack file is therefore not currently admitted, and DOT alone cannot introduce a new execution identity or compiled activity.

Evidence:

- `internal/agent/workflow.go:5`
- `internal/turn/workflow_registry.go:10`
- `internal/graphexec/validate.go:138`
- `internal/turn/research_workflow.go:29`
- `internal/graphexec/registry.go:53`

## f6

Graphs permit custom node names, topology, registered actions and bounded cycles. A filesystem test adds a new Research role and node without Go changes and proves captured bytes survive later edits. The Research graph already runs investigation and shaping in one file. No generic validator enforces four workflow files. Single-pass substeps can be expressed structurally under one existing policy, but that does not grant actual Plan promotion or Implement authority; per-mode semantic restrictions still apply, including fixed Plan creation bounds and review routing.

Evidence:

- `internal/teamsource/research_file_role_test.go:10`
- `internal/teamsource/research/research.dot:1`
- `internal/graphexec/validate.go:13`
- `internal/teamsource/plan_set_graph_test.go:103`

## f7

Markdown-specific inventory: role_sections_test enforces nine headings, order and minimum body size, plus a tenth Skills section for technical Plan; plan_role_guidance_test checks exact guidance phrases, source/embed parity and prose in constraints/context; research_response_contract_test checks response/shared-route wording; plan_set_role_test and plan_contract_test also check wording and section content. These are candidates for moving standard-bundle editorial checks out of core engine tests. No recommendation to delete executable validation is implied.

Evidence:

- `internal/teamsource/role_sections_test.go:196`
- `internal/teamsource/plan_role_guidance_test.go:223`
- `internal/teamsource/plan_role_guidance_test.go:410`
- `internal/teamsource/research_response_contract_test.go:14`
- `internal/teamsource/plan_set_role_test.go:25`
- `internal/teamsource/plan_contract_test.go:286`

## f8

Other Markdown-adjacent tests exercise real engine interfaces: result_contract_examples_test sends fenced contract examples through the shared validator; research_test and plan_contract_test test package metadata, resource closure, digest and invalid references; delivery_plugin_test and bug_analyst_test test rendered provider packages and access. Retain equivalent engine contract tests with small fixture packages even if standard-bundle wording tests move. Research performed static inspection, not test execution or authentic provider observation.

Evidence:

- `internal/teamsource/result_contract_examples_test.go:274`
- `internal/teamsource/research_test.go:46`
- `internal/teamsource/plan_contract_test.go:67`
- `internal/teamsource/delivery_plugin_test.go:16`
- `internal/teamsource/bug_analyst_test.go:91`

## f9

Rights exposes an explicit #317 swap point: Source.Catalogue receives a repository but BuiltinSource ignores it and scans built-in graphs. Reuse this interface with selected installed definitions. lf_rights is restricted to lf_ask and closed actions answer/request_changes/approve, and prohibited in Implement. Installed definitions must preserve these validation and authorization boundaries. Delegated reply acceptance needs separate end-to-end wiring per supplied issue handoff.

Evidence:

- `internal/rights/catalogue.go:27`
- `internal/rights/catalogue.go:120`
- `internal/graphexec/validate.go:248`

## f10

Open design points for shaping: authoritative relationship between catalogue workflow/resource lists and minimal stage-to-file team.config; source identity plus slug/version/digest; missing-mode behavior; update versus repository repinning; linked-version removal and unlink fallback; customization overlays/provenance; compatible old execution paths; rights delegation rollout. The requested installable-team architecture can be established without deciding these now. The source URL was unavailable, so no remote catalogue schema or successful download is claimed.

Evidence:

- `internal/teamsource/research.go:133`
- `internal/config/config.go:725`
- `internal/rights/catalogue.go:27`
