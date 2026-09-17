# Feature Spec: litefactory-documentation

- Status: implementation-ready requirements; Plan publication and readiness remain factory-owned.
- Date: 2026-09-17.
- Authority: owner approved the revised research layout and requested exactly one user story per category and one Markdown deliverable file per task.
- Source: [RESEARCH_BRIEF.md](RESEARCH_BRIEF.md), revision evidence `21d3234719a945435b9685e41881aecefd4af923`; current implementation must be rechecked when writing documentation.

## Goal

- Give prospective and existing litefactory users a GitHub-native documentation entry point, a first-run journey, and discoverable feature, CLI, configuration and operations guidance under `docs/`.

## Category outcomes and scope ownership

| Category | Reader outcome | Owned Markdown deliverables |
|---|---|---|
| Getting started | Understand litefactory, install it, configure GitHub access and complete a first Research journey | `docs/README.md`, `docs/glossary.md`, `docs/getting-started/README.md`, `docs/getting-started/installation.md`, `docs/getting-started/github-setup.md`, root `README.md` |
| Using the factory | Operate the issue workflow and understand feature results and approval boundaries | `docs/guides/README.md`, `docs/guides/workflow.md`, `docs/guides/working-with-issues.md`, `docs/guides/mcp.md`, `docs/guides/attachments.md`, `docs/guides/scheduling.md`, `docs/guides/sync-and-conflicts.md`, `docs/guides/validation-and-delivery.md`, `docs/guides/issue-command-reference.md`; reconcile existing `docs/mcp-adapter.md` and `docs/mcp-tool-families.md` |
| CLI — advanced | Find and use terminal commands for repositories, teams and other supported administration | `docs/cli/README.md`, `docs/cli/repositories.md`, `docs/cli/teams.md`, `docs/cli/command-reference.md` |
| Configuration — advanced | Locate, edit, validate and reload `config.yaml`; understand settings, agents and permissions | `docs/configuration/README.md`, `docs/configuration/config-yaml.md`, `docs/configuration/coding-agents.md`, `docs/configuration/permissions.md` |
| Operations | Monitor usage and daemon state, diagnose failures, recover and clean up safely | `docs/operations/README.md`, `docs/operations/running-the-daemon.md`, `docs/operations/usage-and-costs.md`, `docs/operations/recovery.md`, `docs/operations/cleanup.md`, `docs/operations/troubleshooting.md` |

## Data model and interface surface

| Surface | Required behavior |
|---|---|
| Documentation | Existing Markdown files plus 28 new Markdown pages; relative repository links and GitHub-rendered headings/tables/code blocks |
| Navigation | Root README → `docs/README.md` → five category indexes → shallow topic pages; glossary beside landing page |
| Product APIs, commands, YAML and storage | No runtime changes; document existing interfaces from their current owners |
| Examples | Concrete placeholder issue numbers, paths and credential references; no real credentials, fabricated transcripts or invented installation distribution |
| Permissions | Explain existing owner/bot access and mode limits; documentation creates no authorization and performs no remote actions |

## Business rules and scope boundaries

- Exactly five stories, corresponding to the category rows; shared landing/glossary/root README work belongs to Getting started, not a sixth story.
- Every implementation task owns exactly one named Markdown deliverable, including tasks updating existing files; checks are part of the owning file task rather than standalone non-file tasks.
- Planning artifacts, retained runner evidence and the factory's issue rendering are not product documentation task deliverables; do not create local per-story Markdown.
- Preserve all approved topic pages; this Plan does not exercise the research brief's optional page-merging suggestion.
- CLI owns exact terminal syntax; Configuration owns YAML fields; Guides owns GitHub comment commands and labels; Operations and onboarding link to authoritative references while retaining useful minimal examples.
- Category indexes introduce their category and link to each owned topic; CLI and Configuration visibly carry the advanced label.
- Feature guides cover purpose, prerequisites, a minimal example, expected feedback, common failures and related guidance; references provide complete syntax/defaults without duplicating manuals.
- Preserve existing technical/protocol document paths and anchors wherever retained links use them. Clearly label deep protocol links; do not move existing material into a developer tree.
- Resolve stale root README team-delivery and story-closure claims, and stale MCP availability/future-tense claims, using executable sources and retained tests; historical sections must be labelled and cannot contradict current guidance.
- Existing feature docs provide source material; do not rewrite all legacy technical documentation or change agent instructions.
- Record independently the documented implementation, source/test evidence and authentic runtime observation. Do not claim fresh-install, native-host, billing or remote-publication success from source inspection or synthetic fixtures.

## Acceptance criteria candidates

| ID | Category | Testable result |
|---|---|---|
| AC-001 | Getting started | Root README provides a short accurate introduction and link to `docs/README.md`; landing page explains the issue-driven workflow, representative features and all five category entry points; glossary defines owner, bot, mode, story, run and acceptance. |
| AC-002 | Getting started | Three onboarding pages cover supported macOS prerequisites, source installation and upgrades, Git/gh and agent prerequisites, separate owner/bot identities and required repository/token access, initialization, repository registration, daemon startup and an explicit first Research request, with expected feedback and links to subsequent workflow guidance. |
| AC-003 | Getting started | Root README reconciles obsolete team-delivery and unconditional story-close/reopen claims with current behavior, retains useful existing reference links and distinguishes legacy workflow from separated engineering and acceptance. |
| AC-004 | Getting started | Installation and GitHub permission guidance is checked against current sources and an isolated operator setup; an onboarding walkthrough on a disposable repository records actual versions and observed results. Missing external observation is explicitly retained as unverified, never replaced by a fabricated successful transcript. |
| AC-005 | Using the factory | Workflow and issue guides explain Research, Plan, Implement, Validate and Spike; explicit mode switches, asynchronous questions, plan publication, frozen scope, child refinement/replan and legacy differences; successful output never implies automatic advancement. |
| AC-006 | Using the factory | MCP, attachment and scheduling guides cover their current mode/capability gates, settings cross-links, bounded approvals/receipts, supported file types and limits, precise scheduling controls/timezone behavior and failure/expiry feedback. Factory MCP is distinguished from arbitrary third-party servers. |
| AC-007 | Using the factory | Conflict and validation/delivery guides cover reviewed conflict proposals, engineering versus acceptance versus publication, exceptions, follow-ups, readiness and explicit concrete PR/head merge approval; issue-command reference accounts for all current owner comment controls and canonical labels. |
| AC-008 | Using the factory | Existing MCP adapter/tool-family documents retain their URLs and distinguish historical milestones, currently implemented integrations and unverified authentic-host observations; new MCP guidance and linked existing references agree. |
| AC-009 | CLI | Category index explains terminal conventions and help; repository and team pages document current registration/options/precedence and catalog install/inspect/update/override workflows, retained bundles, drift and force behavior. |
| AC-010 | CLI | Command reference accounts for every command/subcommand/flag in the current registry and handlers, including init, repo, daemon, logs, meter, task, team, story-contract, submission and MCP; descriptor-bound helper commands are labelled advanced factory operations rather than ordinary setup. |
| AC-011 | Configuration | Index explains config location, initialization, editing, precedence, strict validation and restart/reload behavior; field reference covers every supported YAML field with type, default, valid values/range, effect, precedence and reload requirement against template, structs, defaults and validators. |
| AC-012 | Configuration | Coding-agent and permissions pages cover selection, executable paths, defaults and tiers, timeout/conversation behavior, credential references, owner/bot roles, mode permissions, agent-specific restrictions and personal configuration, unredacted local logs versus redacted reports and rejected deprecated permission labels. |
| AC-013 | Operations | Daemon/log and usage pages cover supported lifecycle, login/PATH/reload behavior, status/selectors/filtering, actual versus estimated or unavailable usage, model/descendant coverage, price snapshots, export and historical recovery. |
| AC-014 | Operations | Recovery, cleanup and troubleshooting pages cover eligible quota recovery and limits, cancellation/manual continuation, retained edits/report recovery, retirement versus explicit cleanup, dirty/force behavior and symptom-to-check-to-recovery guidance for documented failure classes. |
| AC-015 | All categories | All 31 owned Markdown files are present or updated; each approved feature has one primary home; every topic is reachable from its category and the landing page; final relative links/anchors resolve, code fences/tables render correctly and no navigation uses the superseded docs/user proposal. |
| AC-016 | All categories | Examples and claims match the implementation revision, contain no literal credentials, retain evidence limitations and do not invent commands/defaults/capabilities; required repository checks are actually executed and retained before engineering completion. |

## Candidate evidence obligations

| Criteria | Proof method | Source availability | Version/context bounds | Missing prerequisite / disposition |
|---|---|---|---|---|
| AC-001–003, AC-005–014 | Scoped document review and source-to-document coverage comparison; inspect owner parsers, CLI registry/handlers, config template/defaults/validators and current workflow/MCP code | Research brief provides concrete file owners and retained revision; repository contains current sources and references | Research anchors are historical navigation aids; implementations must record the revision reviewed and resolve source/context drift | Implementation source reconciliation and completed pages are pending; no live behavior inferred |
| AC-004 | Source review plus separately authorized isolated operator installation/access check and disposable-repository Research walkthrough | Source evidence available; no authentic fresh-install/onboarding capture supplied | Record host, versions, auth/access setup and observed outcome; no guarantee for other hosts/versions | Authentic operator observation remains a future external prerequisite. Do not use the active daemon, edit its state, launch paid nested agents or expand permissions. Retain unverified status if unavailable |
| AC-006, AC-008, AC-012 | Documentation review against current integration paths and explicit evidence labels | Existing code and historical references available; authentic #274/#296 host observations not supplied | Describes implemented capability only; fixtures and source inspection do not establish current host delivery | No new native-host verification promised by this documentation scope; outstanding authentic observations must remain disclosed |
| AC-015 | File inventory, local relative-link/anchor validation and Markdown rendering inspection across final combined docs | Repository files available after implementation | Validate final five-category tree, including cross-story links; scoped intermediate checks may use planned target inventory | Final combined navigation checks pending; incorporate checks into file tasks without extra deliverable files |
| AC-016 | Source review, credential/example inspection and executed `make check` output | Repository required checks available | Offline repository checks cannot prove onboarding, vendor billing or remote publication | Required engineering checks pending; no new unit/integration test files needed for prose-only scope |

## Edge cases and error handling

- Cross-category links point to agreed final paths; final aggregate check must resolve all targets after every category exists.
- Unknown command/config behavior or conflicting historical prose → inspect its executable owner and focused tests; document uncertainty when authentic observation is absent.
- Unavailable binary, invalid YAML, missing bot identity, insufficient repository access, team drift, sandbox restrictions, absent usage evidence, no run/no report, conflicts and uncertain remote effects → explain observable symptoms and supported recovery paths without exposing secrets.
- Rejected attachments do not necessarily fail the run; due time is eligibility rather than a promised start time; receipt acknowledgment is not acceptance/publication; zero usage and unknown usage remain distinct.
- Missing external onboarding evidence → keep AC-004 unverified for later acceptance; no invented pass or implied exception.

## Assumptions and decisions

- Approved five-category layout and feature coverage come from run 0002 plus the current owner approval; no material scope question remains.
- One file per task means each product deliverable task creates or updates one Markdown page; it does not mean all of a category must fit into one file.
- The 28 proposed new pages plus three existing-file reconciliations total 31 deliverables; all are documentation-only.
- Shared navigation and root README fit Getting started because they orient new users; MCP reference corrections fit Using the factory because its guide otherwise links to contradictory material.
- External onboarding verification is a future evidence prerequisite carried from the approved research brief, not permission to operate this machine's active daemon or spend provider resources in this Plan run.
- No new test suite, website toolchain or generated reference system is necessary; use existing repository checks and focused documentation review.

## Out of scope

- Documentation website/static-site generator, hosting, package-manager distribution, new commands/settings/features or runtime fixes.
- Developer/architecture manual, protocol/schema redesign, instruction-tree changes or relocating existing technical references.
- Repairing all unrelated stale code comments or APP_CONTEXT text.
- Paid/live nested-agent probes, authentic-host capture work for #274/#296, active daemon/state changes, remote publication or implementation during Plan.
- Commits, pushes, mode changes, acceptance, frozen scope or readiness established by this spec itself.
