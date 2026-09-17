# User documentation for litefactory — research and first draft

- Request: issue #312, `litefactory-documentation`.
- Mode: Research. Status: complete for feature inventory, documentation proposal and sample copy.
- Date: 2026-09-17; revised in run 0002.
- Repository revision: `21d3234719a945435b9685e41881aecefd4af923`.
- Branch: `lf/312-litefactory-documentation`.
- Intended publication: ordinary Markdown under GitHub `docs/`; no documentation website or developer manual requested.

## Request, scope and decisions

Create a place where a prospective user can understand litefactory, get started, and learn each feature, including MCP and configuration. This run researches the current product and drafts that documentation's structure and representative content. It does not create the final documentation pages, change runtime behavior, or start Plan.

The audience and desired result are sufficiently clear to proceed. In run 0002, the user rejected the mixed `docs/user/` grouping and requested clear categories, explicitly including advanced sections for working with the CLI and `config.yaml`. This revision adopts that decision throughout the tree, feature mappings and sample navigation. The draft uses GitHub-rendered Markdown, relative links and a `docs/README.md` landing page. Existing technical/protocol documents stay at their current URLs; a future `docs/developer/` area remains separate and is not part of this work. No user decision blocks this revised draft.

Success for this run: a source-grounded feature inventory; a proposed destination and page outline for each feature; draft landing and getting-started copy; identified misleading existing claims; and concrete publication checks for a later documentation change. Recommendations below are editorial proposals, not approved engineering stories.

## Findings and recommendation

Use a short root README that points to `docs/README.md`. Organize its navigation into five categories: **Getting started**, **Using the factory**, **CLI (advanced)**, **Configuration (advanced)** and **Operations**. Each category gets one directory with a short README index and directly contained pages. MCP belongs with user-facing factory features; exact terminal commands belong in CLI; YAML fields and permission settings belong in Configuration. Keep existing detailed protocol contracts reachable from relevant advanced pages without mixing them into the main learning path.

The existing root README begins with central team management and then jumps among story progress, conflicts, recovery, diagnostics and contracts. `docs/` contains useful metering, cleanup, troubleshooting and workflow material, but its tracked file inventory has no documentation landing page or general getting-started guide. Evidence: `README.md:1`, `README.md:4`, `internal/cli/commands.go:8`, and the `rg --files docs` inventory at the revision above.

Do not merely rearrange the old prose. Some claims predate current code:

- `README.md:54` says runtime delivery for external teams is not enabled. Current `internal/turn/delivery.go:37` builds selected-mode bundles, `internal/harness/runner.go:642` supplies Claude's plugin directory/bootstrap, and `internal/codex/runner.go:439` supplies native specialist definitions. `internal/turn/delivery_test.go:28` checks the selected roster and identity. The old failure test installs no usable catalog and does not establish that configured current delivery is disabled. Describe the implemented bundle path, with authentic-host verification limits kept separate.
- `docs/mcp-adapter.md:27` says host consumption is unavailable and describes future family work. Current `internal/turn/mcp_runtime.go:88` attaches registration to supported runners; `internal/harness/mcp.go:28` and `internal/codex/mcp.go` expose support. `docs/mcp-tool-families.md:1` already records later native integration while retaining historical handoff text further down. Reconcile the chronology before presenting those pages as current user guidance.
- `README.md`'s story-closing/reopening paragraph at `README.md:68` must distinguish legacy workflow from separated Implement/Validate acceptance. Current delivery guidance requires retained acceptance in addition to a closed issue (`docs/workflow/delivery.md:1`). Do not promise that an engineering result alone closes every story.
- Context and code comments also lag implementation: `.aiteam/APP_CONTEXT.md` describes legacy team installation, `internal/attach/doc.go` says there is no live caller despite `internal/turn/prepare.go:1254`, and comments above the tier table describe Codex as absent although `internal/config/tier.go:156` populates it. Executable paths and focused tests take precedence over these historical descriptions.

These are documentation reconciliation findings, not claims of a runtime defect. No live provider, native MCP host, GitHub publication or first-install success was demonstrated in this research.

## Proposed documentation layout

All new paths below are proposed. Existing technical references remain at their current locations. Five shallow categories replace the original mixed user directory; no additional `advanced/` wrapper is needed. “Advanced” is a navigation label for CLI and Configuration, both still written for users.

```text
docs/
  README.md                         Product overview and five category entry points
  glossary.md                       Shared terms: owner, bot, mode, story, run, acceptance
  getting-started/
    README.md                       First successful Research run; ordered onboarding
    installation.md                 macOS prerequisites, binary setup and upgrades
    github-setup.md                  Owner/bot identities and repository access
  guides/                           Using the factory: everyday issue-based work
    README.md                       Workflow and feature guide index
    workflow.md                     Research, Plan, Implement, Validate and Spike
    working-with-issues.md           Instructions, questions, labels, stories, replanning
    mcp.md                          Factory tools, availability, approvals and receipts
    attachments.md                  Images/documents and rejected-attachment feedback
    scheduling.md                   Delays, dates, dependencies and proposal decisions
    sync-and-conflicts.md            Base sync and reviewed conflict proposals
    validation-and-delivery.md       Acceptance, exceptions, follow-ups and PR approval
    issue-command-reference.md      Exact GitHub comment controls and labels
  cli/                              Advanced: working with the litefactory CLI
    README.md                       CLI conventions, task recipes and reference index
    repositories.md                 Register/list clones; branch and worktree options
    teams.md                        Install, inspect, update and override central teams
    command-reference.md            All registered terminal commands and flags
  configuration/                    Advanced: working with config.yaml
    README.md                       Find/edit the file, precedence, validation and reload
    config-yaml.md                  Complete field reference, defaults and examples
    coding-agents.md                Agent binaries, defaults, model tiers and selection
    permissions.md                  Mode permissions, credentials and data exposure
  operations/                       Keep the factory running and inspect results
    README.md                       Monitoring, recovery and maintenance index
    running-the-daemon.md            Foreground/background service, status and logs
    usage-and-costs.md               Meter summaries, estimates, prices and CSV export
    recovery.md                     Quota interruption, retained edits and report delivery
    cleanup.md                      Retention, retirement preview and manual cleanup
    troubleshooting.md              Symptom → checks → recovery
  ... existing technical references and schemas remain in place ...
```

Category boundaries keep one primary home for each topic. Getting started presents the minimum setup journey and links onward. Guides explain what users do through issues, including slash commands. CLI explains working in a terminal and owns the complete command syntax. Configuration explains file-based customization and owns the full YAML reference. Operations explains diagnosis and ongoing maintenance, with task-focused command examples linked to CLI and settings linked to Configuration. The shared glossary stays beside the landing page.

Category README files are navigation pages, not duplicate manuals. For example, team installation/update steps live in `cli/teams.md`, team-source YAML keys live in `configuration/config-yaml.md`, and the workflow guide explains how specialists participate. The CLI reference lists daemon/meter commands completely, while Operations owns the practical monitoring and recovery guides. Short related topics may share a page during editing, but these category boundaries and the dedicated advanced CLI/configuration areas remain.

## Page contents and feature coverage

Every repository citation in this brief refers to the revision above. Line numbers are navigation anchors, not claims that entire behavior fits on one line.

| Destination under `docs/` | What the reader should learn | Primary evidence |
|---|---|---|
| `README.md`; `getting-started/README.md` | GitHub issues and owner replies trigger selected headless coding agents in task worktrees; the factory reports back. Introduce an explicit Research request before any implementation. | `internal/turn/command.go:192`; `internal/turn/prepare.go:145`; `internal/daemon/mode.go:130` |
| `getting-started/installation.md`; `getting-started/github-setup.md` | macOS GUI-session LaunchAgent; Go source-build requirement if building; Git and authenticated `gh`; separate owner and bot accounts; token references; idempotent init; GitHub repository access. Do not invent a package-manager formula or release download. | `go.mod:3`; `Makefile:15`; `internal/cli/cmd_init.go:64`; `internal/cli/cmd_init.go:133`; `internal/cli/cmd_daemon.go:358` |
| `cli/repositories.md` | Register an existing clone, list registrations, base/prefix/worktree options and their precedence, no checkout mutation, per-task branches, concurrent work, no promise of a nonexistent remove command. | `internal/cli/cmd_repo.go:41`; `internal/cli/cmd_repo.go:52`; `.aiteam/APP_CONSTRAINTS.md:51` |
| `guides/workflow.md` | Explain all five modes and expected output; explicit switches; persistent canonical mode labels; asynchronous questions in Research/Plan; Implement executes checks; Validate reviews retained evidence read-only; successful output does not automatically advance modes. | `internal/agent/mode.go:11`; `internal/daemon/mode.go:171`; `internal/turn/ask.go:112`; `internal/turn/managed_scope.go:65`; `docs/workflow/delivery.md:1` |
| `guides/working-with-issues.md`; `guides/issue-command-reference.md` | Owner identity, body versus follow-up instruction, exact labels, plan discussion/publication, frozen revisions on implementation, child refinements, replan on an idle parent, legacy versus separated workflows. | `internal/turn/command.go:146`; `internal/turn/managed_scope.go:19`; `internal/turn/managed_scope.go:75`; `docs/story-contract.md:1` |
| `configuration/coding-agents.md` | `Coding Agent: Claude` / `Coding Agent: Codex`; default from declared agent order; tier-to-model mapping; absolute binary paths; resumed conversations and new segments when mode/bundle identity changes; configured timeout. Model values are local configuration, not market recommendations. | `internal/daemon/agent.go:11`; `internal/config/template.go:104`; `internal/config/tier.go:156`; `internal/turn/delivery.go:287` |
| `cli/teams.md` | Central catalog, remote sources/local imports, versions and hash pins, mode rosters, repo association, status/update drift and force behavior, operator-global overrides, retained per-run bundles. Explain that a catalog update does not rewrite already-retained conversation resources. | `internal/cli/cmd_team.go:145`; `internal/cli/cmd_team.go:183`; `internal/cli/cmd_team.go:233`; `internal/cli/cmd_team.go:282`; `internal/config/teams.go:28`; `internal/turn/delivery.go:193` |
| `configuration/README.md`; `configuration/config-yaml.md` | `$LF_HOME/config.yaml`; generated canonical template; strict unknown-key handling; credential references; restart requirements; settings/providers/agents/tiers/tool_profiles/teams; defaults, valid range, scope and example for each field. Include attachment, MCP, auto-sync and auto-resume controls. | `internal/config/template.go:13`; `internal/config/config.go:85`; `internal/config/config.go:441`; `internal/config/teams.go:15`; `.aiteam/APP_CONSTRAINTS.md:108` |
| `configuration/permissions.md` | Owner/bot distinction; per-mode ceilings; Validate read-only; Claude tools versus allowed tools; Codex write roots/network and personal-config handling; local logs are unredacted; public report redaction; deprecated permission labels refuse execution. | `internal/config/template.go:114`; `internal/config/config.go:209`; `.aiteam/APP_CONSTRAINTS.md:28`; `internal/daemon/tick.go:1791`; `internal/cli/cmd_logs.go:128` |
| `guides/mcp.md` | An attempt-scoped factory connection for the running agent, tool availability by mode, auto-start setting, user-visible progress and receipts, permitted issue access, explicit Research creation approval, expiry and uncertainty. Separate factory MCP from arbitrary third-party server setup. | `internal/cli/cmd_mcp.go:14`; `internal/turn/mcp_runtime.go:88`; `internal/config/config.go:109`; `docs/mcp-tool-families.md:15` |
| `guides/attachments.md` | PNG/JPEG/GIF/WebP and PDF/DOCX/PPTX; mode/capability gates, count/type/total size limits; ingestion and document interpretation are different; why a file can be rejected without failing the whole run. | `internal/attach/sniff.go:19`; `internal/turn/prepare.go:1254`; `internal/turn/attachments_test.go:90`; `internal/config/config.go:101` |
| `guides/scheduling.md` | Exact `/schedule -by`, `-to`, `--after`, `--propose`, `--accept`, `--reject` syntax; local/explicit timezone and UTC reporting; dependency completion; rejected/expired proposals; due time is eligibility, not promised execution time. | `internal/schedule/schedule.go:57`; `internal/schedule/schedule.go:88`; `internal/schedule/schedule_test.go` |
| `operations/recovery.md` | Opt-in session-limit recovery, at most three automatic invocations, eligibility and cancellation, preserved request/worktree, unknown reset time/manual continuation, retained edits and report-only recovery. | `README.md:114`; `docs/report-recovery.md:1`; `internal/config/config.go:106`; `internal/turn/delivery.go:310` |
| `guides/sync-and-conflicts.md` | Automatic base sync, behind limit, block-on-conflict, optional suggestions, numeric `/confirm`, expiry and regeneration; bounded/sanitized preview versus complete raw statistics; dirty retained edits. | `internal/config/config.go:114`; `internal/turn/prepare.go:158`; `internal/turn/command.go:16`; `README.md:85` |
| `guides/validation-and-delivery.md` | Engineering versus acceptance versus publication; retained findings; exact `/waive`, `/followup`, `/deliver`, `/merge` controls; readiness blockers; concrete PR/head approval; no automatic merge approval from normal instructions. | `docs/workflow/owner-exceptions.md:1`; `docs/workflow/followups.md:1`; `docs/workflow/delivery.md:1` |
| `operations/usage-and-costs.md` | Global/project/task views; actual models and descendants; unknown versus zero; estimates versus provider billing; frozen local prices; filters/CSV; historical recovery preview/apply; coverage limits. | `internal/cli/cmd_meter.go:29`; `internal/cli/cmd_meter_prices.go:16`; `internal/cli/cmd_meter_recover.go:20`; `docs/meter.md:1` |
| `operations/running-the-daemon.md` | Foreground run; start/stop/status; login lifetime; configuration reload through restart; copied PATH; latest tick; app and issue logs, run selector/follow/filter/raw output; diagnostic worker counts versus actual launches. | `internal/cli/cmd_daemon.go:57`; `internal/cli/cmd_daemon.go:358`; `internal/cli/cmd_daemon_agent.go:52`; `internal/cli/cmd_logs.go:128`; `README.md:153` |
| `operations/cleanup.md` | Daily safe retirement, retention age, advisory `task retire`, explicit `task clean`, dirty protection and the destructive `--force` option; merged branch checks and skipped resources. | `internal/cli/cmd_task_retire.go:17`; `internal/cli/cmd_task.go:21`; `docs/retirement.md:1` |
| `operations/troubleshooting.md`; `cli/command-reference.md`; `glossary.md` | Missing bot identity, bad YAML, unconfigured agent, unavailable binary, sandbox cache/network restrictions, no run versus no report, conflicts, uncertain publication, team drift, missing metering evidence; complete command/label index; legacy contract upgrade preview/apply. | `internal/cli/commands.go:8`; `internal/cli/cmd_story_contract.go:25`; `docs/codex-initialization-troubleshooting.md:1`; `docs/report-recovery.md:1` |

The advanced CLI category starts at `docs/cli/README.md` with terminal conventions, finding help and task-oriented links. Its `command-reference.md` must account for all registered groups: `init`; `repo add/list`; `daemon run/start/stop/status`; `logs`; `meter`, `meter export/recover/prices list/prices set`; `task clean/retire`; `team list/show/add/status/update/manage/validate`; `story-contract upgrade`; and the advanced, factory-supplied `submission preflight/submit/receipt` and `mcp serve` commands. The last two groups are not manual onboarding steps and require an authorized attempt descriptor. Source: `internal/cli/commands.go:8`.

## Draft landing page: docs/README.md

The following is proposed user-facing copy; links target the proposed tree and do not yet exist.

> # litefactory documentation
>
> litefactory turns GitHub issues into a guided software workflow. Describe the work in an issue, choose a mode, and continue the conversation in comments. The factory runs Claude Code or Codex in a dedicated Git worktree and reports the outcome back to the issue.
>
> Start with Research to understand a problem, use Plan to agree on the work, and move to Implement when the plan is ready. Validate reviews the resulting evidence. Spike supports a focused experiment or small ad-hoc change. You control the workflow and review the work before approving delivery.
>
> **New to litefactory?** Follow [Getting started](getting-started/README.md) to set up your machine and run your first Research request.
>
> ## What you can do
>
> - Run issue-driven work across registered repositories while keeping each task in its own worktree.
> - Choose Claude Code or Codex and configure models, permissions and specialist teams.
> - Turn plans into tracked stories, review acceptance findings, and approve delivery through a pull request.
> - Schedule work, recover eligible quota interruptions, and resolve base-branch conflicts with reviewed proposals.
> - Include supported images and documents in requests.
> - Inspect logs, token usage and cost estimates, then clean up finished task resources.
> - Let running agents use scoped factory MCP tools for progress, results and permitted issue operations.
>
> ## Explore the documentation
>
> | Category | Start here |
> |---|---|
> | **Getting started** | [Set up your first repository and Research run](getting-started/README.md), with [installation](getting-started/installation.md) and [GitHub setup](getting-started/github-setup.md). |
> | **Using the factory** | [Everyday workflow and features](guides/README.md): modes, issues, MCP, attachments, scheduling, conflicts and delivery. |
> | **CLI — advanced** | [Work with the litefactory CLI](cli/README.md): repository and team management, command recipes and the [complete command reference](cli/command-reference.md). |
> | **Configuration — advanced** | [Work with config.yaml](configuration/README.md): file location, editing and reload, the [YAML reference](configuration/config-yaml.md), coding agents and permissions. |
> | **Operations** | [Run and maintain the factory](operations/README.md): daemon and logs, usage and costs, recovery, cleanup and troubleshooting. |
>
> Looking for a specific term or GitHub control? See the [glossary](glossary.md) or [issue-command reference](guides/issue-command-reference.md).
>
> litefactory currently runs as a macOS user service. You need access to the GitHub repository, a separate bot identity, and an authenticated supported coding agent. See [Installation](getting-started/installation.md) and [GitHub setup](getting-started/github-setup.md) for prerequisites.

## Draft getting-started page: docs/getting-started/README.md

This is a prose/command draft, not an executed installation transcript. External tool installation and minimum GitHub token permissions need a verified companion installation/access guide before final publication. No guessed release URL or package-manager command is included.

### Before you start

Use a Mac with a logged-in desktop session, Git, GitHub CLI authenticated as the owner, and the coding agent you plan to use installed and authenticated. The bot must use a different GitHub account from the owner. The local project clone must have the intended GitHub remote and usable Git credentials for the factory's branch operations.

The installation guide should offer a source build from a checked-out litefactory revision as the repository-supported fallback. `go.mod` declares Go 1.26. Build into a dedicated executable location and put that location on PATH; do not instruct an upgrade to overwrite an executable currently used by a running daemon. A maintained release installer/distribution channel has not been established by this investigation.

### Initialize the factory

Run:

```sh
litefactory init
```

This creates the local configuration and database under `~/.litefactory`, unless you selected another location with `LF_HOME`. It discovers the owner from your authenticated `gh` account. A missing bot token produces setup guidance; initialization can be rerun without overwriting an existing configuration.

For the generated macOS Keychain reference, store the bot token at the prompt and rerun initialization:

```sh
security add-generic-password -s litefactory -a github-bot -w
litefactory init
```

Confirm that the summary shows two different identities. Configuration stores the Keychain service/account or an environment variable name, never a literal token. See [GitHub setup](github-setup.md) for the access this bot needs and credential troubleshooting.

### Choose the agent and register a repository

Review `~/.litefactory/config.yaml`: configure the installed agent binaries and model tiers, and make the intended default the first configured agent. Use [Coding agents](../configuration/coding-agents.md) when selecting a different agent for an issue. Check [Permissions](../configuration/permissions.md) if your project's builds require compiler-cache directories or network access. The advanced [Configuration category](../configuration/README.md) explains the complete file and reload behavior.

Register an existing local project clone:

```sh
litefactory repo add /absolute/path/to/project
litefactory repo list
```

The list shows the registered remote, base branch and worktree location. See the advanced CLI guide [Repositories](../cli/repositories.md) to override these defaults. For repositories using a central specialist team, follow [Teams](../cli/teams.md) to install and validate that team, then associate it using `repo add ... --team <team-id>`. The onboarding guide should include a tested team path once the intended distribution's availability has been verified; the basic registration command does not silently select a team.

### Start litefactory

```sh
litefactory daemon start
litefactory daemon status
```

The factory runs in your macOS login session and starts again at login. Check that the service is running and inspect logs if it does not complete a polling tick. Configuration is loaded at startup; after changing it, follow [Running the daemon](../operations/running-the-daemon.md) for the stop/start procedure.

### Ask for your first research run

As the configured owner, create an issue in the registered repository with a clear description of the task. Put this explicit request in the issue body or an owner comment:

```text
/research Explain how this project's configuration works and identify where a new setting would belong. Report your findings without changing application code.
```

Using an explicit command makes the intended mode clear. Watch the issue for the factory's response. Research may ask material questions; reply on the issue to continue. A research result does not itself move the feature into Plan or Implement.

If you need local diagnostics, replace `123` below with your issue number:

```sh
litefactory logs 123
```

Use a qualified issue selector when the number is ambiguous across registered repositories. Keep raw local logs private; they are not redacted like published reports.

You have completed this guide when the repository is registered, the daemon is running, and the bot has reported a Research result or a concrete clarification question on your issue. Next, read [Workflow](../guides/workflow.md) to understand planning and approval before requesting implementation. The [Using the factory index](../guides/README.md) covers other everyday features; advanced CLI and YAML details are available when you need them.

## Draft feature page patterns

Every feature guide should start with the outcome it enables, then prerequisites, a minimal example, expected feedback, common failures and related pages. References add exact syntax/defaults; introductory guides do not expose implementation types or database details.

### Advanced configuration category: docs/configuration/README.md

“Use configuration to choose your coding agents, control concurrency and worktree placement, and set the permissions available in each workflow mode. `litefactory init` writes a commented starting configuration. Edit the file under `LF_HOME`, then restart the daemon to load the change. Unknown keys and invalid values are errors.”

Example of a narrowly scoped edit, explicitly merged into the existing `settings` mapping:

```yaml
settings:
  max_concurrent_runs: 2
  auto_resume_on_session_limit: true
```

The category index should link the editing/reload steps to `config-yaml.md`, `coding-agents.md` and `permissions.md`. Its `config-yaml.md` reference should have one row per supported YAML field with type, default, valid range, effect, precedence and reload requirement. Derive it from `DefaultFileContents`, `Defaults`, structs and validators together. Do not copy a full second template that drifts independently. Explain that concurrency is a maximum and recovery still requires verified quota/reset evidence.

### Advanced CLI category: docs/cli/README.md

“Use the litefactory CLI to register repositories, manage specialist teams and inspect the factory from your terminal. Start with the task you want to perform, or use the command reference to look up exact arguments and flags.”

The category index links to [Repositories](repositories.md), [Teams](teams.md) and the [Command reference](command-reference.md). Task links point to [daemon and logs](../operations/running-the-daemon.md), [usage and costs](../operations/usage-and-costs.md) and [cleanup](../operations/cleanup.md) in Operations. Explain that GitHub slash commands are documented separately in [Issue commands](../guides/issue-command-reference.md). Keep factory-supplied descriptor commands in an advanced subsection of the command reference; they are not ordinary setup commands.

### MCP guide: docs/guides/mcp.md

“litefactory supplies running agents with a scoped connection to factory operations. Depending on the mode, an agent can report progress, submit its result, inspect permitted issue summaries and recover the recorded outcome of a request. This connection belongs to the current run; it is not a general GitHub administration service.”

Explain that `settings.mcp_auto_start` defaults to true, whereas `settings.allow_mcp_story_creation` defaults to false. Showing a tool does not authorize every call. List the mode-specific families from current registration and the user-visible purpose of each, with links to `../configuration/config-yaml.md` for settings and the retained advanced tool-family reference for raw arguments.

For Research issue creation, the journey is: the owner enables the creation setting and restarts; a concrete creation envelope is prepared through `/mcp-create prepare ...`; the factory presents exact content and a digest; the owner approves the displayed digest and relationship using `/mcp-create <digest> linked` or `attach`; the next eligible authorized launch can consume that approval. Ordinary users should copy the exact reviewed proposal, not author guessed internal hashes. Expired or changed proposals need new approval. Publication can remain pending or uncertain; a receipt or progress message does not establish acceptance or successful remote delivery. Sources: `docs/mcp-tool-families.md` sections “Exact creation preparation and publication” and “Request identity and recovery”; `internal/turn/mcp_runtime.go:295`.

### Workflow guide: docs/guides/workflow.md

“Work advances through explicit stages. Research explains the problem; Plan describes the proposed solution and stories; Implement makes changes and runs checks; Validate reviews retained evidence; delivery requires approval of a concrete pull request. You can request a stage with a mode command. Some transitions require the previous stage's retained result and approved scope.”

Include one walkthrough using `/research <request>`, `/plan <request>`, `/implement <request>` and `/validate <request>`, explaining what the user should inspect before each step. Do not present a bare slash command without required text. Link separate exact controls for exceptions, follow-ups and merging. Call out legacy workflow differences where observable, without making users learn transport schema versions.

## Existing documentation disposition

- Reuse user-relevant material from `docs/meter.md`, `docs/retirement.md`, `docs/report-recovery.md`, `docs/codex-initialization-troubleshooting.md`, and `docs/workflow/{delivery,followups,owner-exceptions}.md` after checking current behavior. Route metering, retirement, report recovery and troubleshooting into Operations, and delivery/follow-up/exception walkthroughs into Using the factory. Link deeper references to preserve existing URLs.
- Keep `docs/agent-result-contract.md`, `docs/agent-auxiliary-contracts.md`, `docs/plan-outcome.md`, `docs/story-contract.md`, schemas, evidence/cost capture pages and instruction manifests as advanced protocol material. A later developer-docs project can organize these; this project should not rewrite them into an architecture manual.
- Reconcile dated MCP adapter/family statements and root README claims. Mark historical sections as such or update them with source-backed current behavior. Do not erase unresolved authentic-host evidence requirements.
- Add a root README documentation link and short product introduction during the later documentation implementation. Keep all main user content under `docs/` as requested.

## Options, recommendation and publication checks

The user has settled the need for distinct categories and dedicated advanced CLI/configuration areas. Recommend the five shallow directories shown above, reached from `docs/README.md`, with a single primary home per topic and relative links between categories. This replaces the initial mixed user-directory proposal. A static-site generator is unnecessary for the requested GitHub documentation. Retain existing reference paths and leave future developer documentation separate.

Before publishing the actual pages:

1. Recheck the current CLI registry, config template/validators and owner-control parsers; command examples must match their selected revision. Verify complete command and configuration coverage against those sources.
2. Confirm the installation path and GitHub permission matrix against an isolated operator setup. The current research does not certify a fresh install, remote catalog availability or vendor authentication. These are implementation verification tasks, not unresolved editorial choices.
3. Walk through onboarding on a disposable repository and record actual versions. Use no existing daemon state for tests. Explain expected output at each step without fabricated transcripts.
4. Verify relative links, GitHub rendering and navigation. Every feature above must be reachable through the five category entry points. Check that CLI and Configuration are visibly marked advanced, GitHub controls stay in Guides, each mapped topic has one primary home, and no current sample links still use the superseded directory. Label advanced protocol links clearly.
5. Keep examples free of literal credentials and invented commands. Distinguish estimates/partial collection, engineering/acceptance, and pending/confirmed remote effects.
6. Preserve old links; reconcile stale claims; avoid describing native integrations as proven beyond retained evidence. Run the repository's required checks when making a later committed documentation change, recording what was actually executed.

No code, docs implementation, agent instructions or daemon state were changed in this Research run. Only this brief and its session manifest were written. No checks or paid/live-provider runs were executed; source/test inspection does not establish runtime test success.

## Probe environment, evidence limits and history

Observed environment: macOS/Darwin arm64; `go version go1.27.0 darwin/arm64`; repository module declares Go 1.26. Probes were filesystem reads, focused `rg` searches, `git rev-parse HEAD`, OS/tool version queries and the byte inventory below. No external web research was needed to establish this checkout's behavior. No build, daemon invocation, network publication, native-host capture or `make check` occurred. Missing-path lookups were corrected by locating their actual owners; they do not establish missing functionality. No sandbox denial was treated as a product limitation.

Research complete means the requested inventory and first documentation draft are ready for review. It does not mean user documentation has been implemented or published, nor that a later mode was selected. Suggested next step: agree on this structure, then explicitly commission the documentation implementation through the normal workflow.

- 2026-09-17, run 0001: Initial Research completed; inventoried feature owners and existing docs, recorded stale claims, drafted proposed tree, landing page, getting-started path and feature-page patterns. No material user questions remain for this draft.

- 2026-09-17, run 0002: User requested clearer clustering and dedicated advanced CLI/config.yaml categories. Revised recommendation, complete tree, feature destinations, landing navigation and sample guide links consistently. Preserved original evidence findings and limitations; no source reinvestigation, runtime checks, docs implementation or new mode. Research revision complete.

### Source-byte inventory

The retained run-0001 focused evidence corpus contains 50 distinct source/document files, totaling **683,709 bytes** on disk. This measures full file lengths once, even where only relevant excerpts were inspected. It is not bytes emitted by tools, a count of all discovery reads, or a billed-token measurement. Billed input tokens are unavailable in this research environment and are not inferred from source bytes.

| Source | Bytes |
|---|---:|
| `.aiteam/APP_CONSTRAINTS.md` | 34,943 |
| `Makefile` | 2,667 |
| `README.md` | 11,757 |
| `docs/codex-initialization-troubleshooting.md` | 2,748 |
| `docs/mcp-adapter.md` | 15,034 |
| `docs/mcp-tool-families.md` | 17,050 |
| `docs/meter.md` | 12,654 |
| `docs/report-recovery.md` | 2,992 |
| `docs/retirement.md` | 3,706 |
| `docs/story-contract.md` | 10,365 |
| `docs/workflow/delivery.md` | 3,559 |
| `docs/workflow/followups.md` | 1,945 |
| `docs/workflow/owner-exceptions.md` | 3,387 |
| `go.mod` | 685 |
| `internal/agent/mode.go` | 1,774 |
| `internal/attach/sniff.go` | 6,317 |
| `internal/cli/cmd_daemon.go` | 17,976 |
| `internal/cli/cmd_daemon_agent.go` | 15,016 |
| `internal/cli/cmd_init.go` | 12,194 |
| `internal/cli/cmd_logs.go` | 37,295 |
| `internal/cli/cmd_mcp.go` | 1,374 |
| `internal/cli/cmd_meter.go` | 12,313 |
| `internal/cli/cmd_meter_prices.go` | 4,455 |
| `internal/cli/cmd_meter_recover.go` | 5,535 |
| `internal/cli/cmd_repo.go` | 12,562 |
| `internal/cli/cmd_story_contract.go` | 5,694 |
| `internal/cli/cmd_task.go` | 6,146 |
| `internal/cli/cmd_task_retire.go` | 3,357 |
| `internal/cli/cmd_team.go` | 17,073 |
| `internal/cli/commands.go` | 1,079 |
| `internal/codex/mcp.go` | 5,870 |
| `internal/codex/runner.go` | 23,434 |
| `internal/config/config.go` | 37,753 |
| `internal/config/teams.go` | 3,175 |
| `internal/config/template.go` | 8,712 |
| `internal/config/tier.go` | 7,798 |
| `internal/daemon/agent.go` | 6,288 |
| `internal/daemon/mode.go` | 9,256 |
| `internal/daemon/tick.go` | 90,904 |
| `internal/harness/mcp.go` | 3,095 |
| `internal/harness/runner.go` | 48,227 |
| `internal/schedule/schedule.go` | 8,571 |
| `internal/turn/ask.go` | 6,539 |
| `internal/turn/attachments_test.go` | 4,503 |
| `internal/turn/command.go` | 10,015 |
| `internal/turn/delivery.go` | 19,024 |
| `internal/turn/delivery_test.go` | 10,432 |
| `internal/turn/managed_scope.go` | 9,443 |
| `internal/turn/mcp_runtime.go` | 14,971 |
| `internal/turn/prepare.go` | 72,047 |
