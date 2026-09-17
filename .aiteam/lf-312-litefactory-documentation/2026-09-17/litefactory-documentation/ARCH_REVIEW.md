# Architect: Go Architect
# Created: 2026-09-17

## Verdict

Architecture supports the documentation-only proposal. No runtime change, new package, schema, dependency or standard deviation is needed. Five category stories and one Markdown deliverable per task are consistent with the approved scope. Source-based documentation can proceed; authentic onboarding observation is unavailable and remains an external evidence prerequisite for AC-004, not a permission to operate the current daemon.

No architect is rostered for documentation information design. The engineer should follow `.aiteam/APP_CONSTRAINTS.md` and their own judgment, flagging assumptions in their task notes. The category and page boundaries below come from the approved feature spec; this review addresses their correspondence to existing Go interfaces and evidence boundaries, not a new editorial design.

## System Overview

### How This Feature Fits

Markdown readers enter from root `README.md`, continue through `docs/README.md`, and choose among five category indexes. These pages describe the existing CLI, configuration, issue workflow, MCP and operational surfaces. They neither call nor modify those interfaces. The existing technical references remain at their current paths and remain clearly identified as deeper material.

The Go owners remain `internal/cli` for terminal command registration and handlers, `internal/config` for YAML/defaults/validation/path resolution, `internal/daemon` for mode selection/admission/time, `internal/turn` for individual run coordination and owner controls, `internal/agent` for neutral runner contracts, `internal/harness` and `internal/codex` for actual provider adapters, and `internal/store` for durable state. Documentation must reconcile against executable paths and focused tests rather than relying on possibly stale descriptive comments.

### New Packages / Components

None. The 28 new Markdown pages and three existing-file reconciliations form a documentation layer. No site generator, reference generator, test dependency, new service or local per-story Markdown is required. Managed stories remain factory-owned GitHub records under CONC-12.

### Key Boundaries

- Terminal syntax has one primary home in `docs/cli/`; YAML settings in `docs/configuration/`; GitHub comment controls and canonical labels in `docs/guides/`. Onboarding and Operations include minimal task examples and link to these references.
- Root README, landing page and glossary belong to Getting started. Existing `docs/mcp-adapter.md` and `docs/mcp-tool-families.md` reconciliation belongs to Using the factory. This keeps every file owned by exactly one category without creating a sixth story.
- Runtime team delivery and MCP attachment describe code capability; authentic current-host delivery is a separate assertion. Source and synthetic tests cannot establish it (EXT-4, EXT-5).
- Input receipts, engineering, acceptance, remote publication and readiness remain separate concepts throughout all categories (SEC-21, CONC-10–12, CONC-22).

### Data Flow

Documentation flow: root README → documentation landing → category index → topic → related authoritative syntax/settings or retained technical reference.

Evidence flow: current executable owner and focused tests → source-to-document comparison → retained check outcome tied to implementation snapshot → later read-only Validate assessment. The planning research revision is a navigation baseline, not proof that future implementation bytes match it. Product documentation does not become an alternate executable contract or persisted state.

### Concurrency & Context

No new concurrency or contexts. Pages describing scheduling/recovery must preserve eligibility versus actual dispatch, daemon-owned time and independent delivery debt. No new live daemon requirement is introduced. Cross-category links can be checked against the fixed target inventory during scoped work; final combined links and anchors require all pages. Navigation references alone do not justify serializing otherwise independent stories.

### Shared State and Side Effects

Only the 31 named documentation files are product deliverables. The active daemon, `dist/`, runtime configuration/database, registered clones and remote issues are not verification sandboxes. Documentation examples must never contain literal credentials or permission-bypass invocations (SEC-1, SEC-4, SEC-6, CFG-4). Source-install examples use a separate binary location; upgrades must not imply overwriting an active executable.

## Cross-story Decisions

1. Keep the approved five categories, exact page paths and single file ownership. Landing/glossary/root README are Getting started; existing MCP references are Using the factory.
2. Keep commands, YAML and owner controls at their designated primary homes; link from recipes instead of maintaining competing full references.
3. Preserve existing technical URLs/used anchors, reconcile the three scoped stale documents, and leave unrelated legacy references and instruction trees outside scope.
4. No runtime, dependency, generator or automated provider-probe changes. Retain source revision, actual check results and evidence limitations independently.
5. Treat all category stories as independently authorable against the fixed navigation contract. Final combined link/render checks and required repository checks belong to implementation evidence within the file tasks and aggregate gate; do not invent an extra non-file task.
6. AC-004 still needs a separately authorized isolated operator setup and disposable-repository observation. Do not manufacture a pass or weaken its obligation. Existing #274/#296 authentic-host gaps are disclosed, not made new implementation requirements.

## Per-deliverable Go Surface Hints

No Go implementation tasks exist in this scope. Every deliverable is owned by `go-implementation-engineer`, the supplied implementation role. The table maps each approved file to reusable interfaces and a concrete pitfall for documentation source review; task IDs and story content remain the plan author's responsibility.

| Approved Markdown deliverable | Existing interface / review focus |
|---|---|
| `docs/README.md` | `internal/turn/command.go`, `internal/daemon/mode.go`: issue-driven execution and five explicit modes; output is not a mode transition. |
| `docs/glossary.md` | `internal/agent`, `internal/turn`, `internal/store`: distinguish run, story, engineering, acceptance and delivery. |
| `docs/getting-started/README.md` | `internal/cli/cmd_init.go`, `cmd_repo.go`, `cmd_daemon.go`: ordered first Research path; setup examples are not an observed transcript. |
| `docs/getting-started/installation.md` | `go.mod`, `Makefile`, `internal/launchd`: source build and user LaunchAgent prerequisites; no invented release channel or active binary overwrite. |
| `docs/getting-started/github-setup.md` | `internal/identity`, `internal/secrets`, `internal/github`: separate numeric identities, reference-only credentials and actual access needs; isolated observation remains outstanding. |
| `README.md` | `internal/turn/delivery.go`, current runners and workflow delivery references: remove obsolete team-disabled and unconditional closure claims while preserving useful links. |
| `docs/guides/README.md` | Link all approved guide targets and exact owner-control reference; avoid embedding duplicate command catalogues. |
| `docs/guides/workflow.md` | `internal/agent/mode.go`, `internal/daemon/mode.go`, `internal/turn/ask.go`, `managed_scope.go`: persistent selection, asynchronous questions and explicit transitions. |
| `docs/guides/working-with-issues.md` | `internal/turn/command.go`, `managed_scope.go`, `story_refinements.go`: owner instructions versus untrusted context, frozen children and parent replanning. |
| `docs/guides/mcp.md` | `internal/turn/mcp_runtime.go`, `internal/harness/mcp.go`, `internal/codex/mcp.go`: attempt-scoped factory attachment, gated tools, receipt dimensions and expiry. |
| `docs/guides/attachments.md` | `internal/attach/sniff.go`, `internal/turn/prepare.go`, `attachments_test.go`: inspect live call paths rather than stale package commentary; ingestion is not guaranteed interpretation. |
| `docs/guides/scheduling.md` | `internal/schedule/schedule.go` and tests: exact controls/timezones/dependencies; due time is eligibility. |
| `docs/guides/sync-and-conflicts.md` | `internal/turn/prepare.go`, `command.go`, conflict functions and report previews: sanitized previews differ from complete raw apply data; preserve dirty-work recovery. |
| `docs/guides/validation-and-delivery.md` | `internal/turn/acceptance.go`, `feature_delivery.go`, `owner_exceptions.go`, `followups.go`: explicit concrete PR/head approval, retained acceptance and legacy separation. |
| `docs/guides/issue-command-reference.md` | `internal/turn/command.go` and daemon label parsers: exhaustive current control/label inventory; terminal flags belong to CLI. |
| `docs/mcp-adapter.md` | Existing adapter history versus current `mcp_runtime.go` and runner support; retain URL/used anchors and label superseded milestones. |
| `docs/mcp-tool-families.md` | Existing native-family contract versus current attachment paths; reconcile future tense while preserving authentic-host limits. |
| `docs/cli/README.md` | `internal/cli/commands.go`: terminal conventions/help and advanced label; descriptors are not ordinary onboarding. |
| `docs/cli/repositories.md` | `internal/cli/cmd_repo.go`, `internal/repos`, `internal/naming`: registered clone stays intact, precise base/prefix/worktree precedence, no invented removal command. |
| `docs/cli/teams.md` | `internal/cli/cmd_team.go`, `internal/config/teams.go`, `internal/turn/delivery.go`: current catalog/versions/pins/overrides and retained bundles; older context may describe superseded installation behavior. |
| `docs/cli/command-reference.md` | `internal/cli/commands.go` plus all registered handlers: complete subcommand and flag inventory including helper/MCP commands with descriptor requirements. |
| `docs/configuration/README.md` | `internal/config/config.go`, `template.go`, `Paths`: location, initialization, precedence, validation and restart requirements; do not promise hot reload (CFG-9). |
| `docs/configuration/config-yaml.md` | Current structs, template, defaults and validators including `teams.go`: every accepted field and bounds; omitted versus explicit zero matters (CFG-1–6). |
| `docs/configuration/coding-agents.md` | `internal/config/tier.go`, `internal/daemon/agent.go`, current provider runners: configured selection/tier/path/timeouts/conversation identity; no market model recommendations. |
| `docs/configuration/permissions.md` | `internal/config/policy.go`, provider adapters, `internal/daemon/tick.go`, `cmd_logs.go`: tool boundaries, roots, owner/bot distinction, rejected labels, unredacted local logs and redacted reports. |
| `docs/operations/README.md` | Link every operational topic and its authoritative CLI/configuration counterpart. |
| `docs/operations/running-the-daemon.md` | `internal/cli/cmd_daemon.go`, `cmd_daemon_agent.go`, `cmd_logs.go`, `internal/launchd`: login lifetime/PATH/restart/log selectors; worker counters are not actual launches. |
| `docs/operations/usage-and-costs.md` | `internal/cli/cmd_meter.go`, price/recovery handlers, `internal/meter`, `internal/store/meter.go`: unknown versus zero, actual versus estimated attribution, immutable price snapshots and coverage bounds (CONC-18–20, EXT-5). |
| `docs/operations/recovery.md` | `internal/turn/continuation_dispatch.go`, daemon continuation paths and report recovery: opt-in quota limits, cancellation and retained edits; attempts and announcements settle independently (CONC-13–17). |
| `docs/operations/cleanup.md` | `internal/cli/cmd_task_retire.go`, `cmd_task.go`, `docs/retirement.md`: advisory retirement versus explicit cleanup and force; preserve ownership and branch checks (SEC-7). |
| `docs/operations/troubleshooting.md` | Existing initialization/report recovery references and owning handlers: symptom → supported check → recovery; no environment widening or invented successful remote effect. |

### Platform Notes

- Platform: package layout, errors, concurrency, interfaces, dependencies, HTTP/API, database and logging changes — not applicable (prose).
- Platform: testing — existing repository `make check` is the required engineering gate; no new unit/integration suite or paid-provider test is warranted for Markdown. Retain the actual output and failures honestly.
- Platform: formatting — final relative links, anchors, fences and tables require document checks. Go formatting/lint remain the existing repository checks, not a new documentation toolchain.

### Security Findings

**Scope:** Forward-looking risks introduced by the proposed documentation, not an audit of runtime code.
**Rulebook:** `.aiteam/APP_CONSTRAINTS.md`, especially SEC-1–9, SEC-20–21, CFG-4 and EXT-4–5.

| Area | Status | Finding |
|---|---|---|
| Input validation | Compliant plan | Exact syntax/examples are reconciled against existing parsers; prose creates no parsing surface. |
| Authentication and authorization | Concern to retain | Access requirements need source review plus external onboarding observation; do not imply a guide grants approval or a receipt authorizes publication. |
| Secrets/configuration | Compliant plan | Credential references/placeholders only; no real credentials, bypass strings or active-state edits. |
| Data handling | Concern to retain | Local logs are unredacted; public reports are separately redacted. Example output must not leak private machine/user values. |
| Dependencies/supply chain | Compliant plan | No new dependencies or invented package-manager distribution. Source-install guidance is revision bounded. |
| Error handling | Compliant plan | Document observable symptoms and supported recovery while preserving uncertainty and failure causes. |

### Evidence Availability and Acceptance Boundary

Repository source, focused tests and historical documentation are available for all categories. Their availability is not a statement that checks have run in this Plan. Source correspondence, inventory, link/anchor/render checks, example checks and `make check` must be executed and retained during implementation. Later Validate remains read-only (SEC-21, EXT-4).

AC-004 has no supplied authentic isolated-installation/disposable-repository walkthrough. Record host/tool versions, authorization context and observed result if the authorized operator later supplies it; otherwise leave that criterion unverified. This is an acceptance evidence gap, not a new runtime development task, not permission to run the active daemon, and not a reason to fabricate observations. The affected story must carry this inventory, not only the shared plan brief.

AC-006/008/012 may establish accurate documentation of implemented MCP/team/permission paths from source review while explicitly disclosing missing authentic #274/#296 host observations. Those historical external observations are not newly required by this documentation scope. Usage claims similarly distinguish provider observations from complete billing or descendant certification.

No architecture deviations requested. The task-decomposer's generic separate-test-task convention does not apply: this owner explicitly requires one Markdown file per task, and the scope needs document checks rather than new test files. Checks remain part of each owning task and existing aggregate engineering evidence.
