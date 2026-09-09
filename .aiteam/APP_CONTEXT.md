# APP_CONTEXT — litefactory

**Lookup reference:** what exists and where. Normative rules live in
[`APP_CONSTRAINTS.md`](APP_CONSTRAINTS.md).

`litefactory` (module `github.com/cloudvane/litefactory`, **Go 1.26**) is a macOS
daemon that polls GitHub issues and, when the owner comments, runs a headless
configured Claude or Codex process inside a git worktree and reports back on the thread.

Single Go module. Not a monorepo. Everything is under `internal/`, with
`cmd/litefactory/main.go` as wiring only (umask, signal context, logger,
dispatch, exit code).

---

## Before you run anything

- `dist/litefactory` may be a live daemon; safe builds use `go build ./...`.
- `make check` runs fmt, vet, pinned golangci-lint v2 and race tests; `go test ./...` is the quick pass.
- Tests use no integration tags. Runtime state is under `$LF_HOME` (default `~/.litefactory`). See the working rules for build/runtime prohibitions.

---

## Package map and dependency direction

Arrows point at what a package imports (litefactory packages only).

```
config, naming, store, git, secrets, identity, launchd, logging, render,
gittest, planbrief, attach, schedule → leaves

agent         → naming

redact        →  secrets
team          →  config             write engine, index decode/validate, drift classifier (Status/Classify), per-repo overrides (team.overrides.json)
github        →  naming, redact, secrets
codex         →  agent, config, naming
harness       →  agent, config, naming
report        →  agent, git, naming, redact
repos         →  git, naming, store
turn          →  agent, attach, codex, config, git, github, harness, naming, planbrief, redact, report, store
daemon        →  agent, attach, codex, config, git, github, harness, naming, redact, report, repos, secrets, store, turn
teamsource    →  render, repos, store, team   resolves + renders a team's roster (3-tier index precedence)
cli           →  config, daemon, git, github, identity, launchd, naming, repos, secrets, store, team, teamsource
cmd/litefactory → cli, config, logging
```

Useful boundary checks: `config/purity_test.go` pins the configuration leaf;
`github/surface_test.go` pins the forge API inventory; `turn/gate_test.go` pins
clone-gate call sites. `internal/agent` holds both runners' shared contracts;
`internal/report` renders values while `internal/turn` delivers them.

### Read the `doc.go` files

Package specifications live in `doc.go`; `config` and `store` use the comments atop `config.go` and `store.go`.

---

## Do not reinvent these

| I need to… | Use | Notes |
|---|---|---|
| plan proof for coding-agent behavior | [Nested-agent evidence decision](../docs/decisions/nested-agent-evidence.md) | Evidence inventory, external capture route and historical claim limits |
| resolve `$LF_HOME` or any path under it | `config.Locate()` → `config.Paths` | `ConfigPath DBPath LogsDir RunsDir DaemonLogPath LockPath SettingsPath` |
| derive a slug, branch, worktree path, run log path, claude session id | `internal/naming` | `Ref`, `ParseRef`, `Slug`, `Branch`, `ClaudeSessionID` (UUIDv5, pinned namespace), `WorktreePath`, `RunLogPath`, `RunMetaPath`, `DefaultWorktreeRoot`. All pure; never reads `LF_HOME` |
| run any SQL | `internal/store` | `Open(ctx, path)` then **`Migrate(ctx)` explicitly** — `Open` does not migrate. Accessors are one file per entity |
| run any git command | `internal/git` | `Resolve(ctx)` → `Binary.Open(ctx, dir)` → `Clone` (reads) / `Worktree` (mutates). `EnsureWorktree`, `ParseRemoteURL`, `*CommandError`; `Status` preserves raw index; `MergeBase` guards staged/tracked or divergent reserved transport |
| serialise git ops across concurrent turns on one registered clone | `internal/daemon`'s `cloneGate`, via `turn.Options.CloneGate` | Unexported in `daemon`; `internal/turn` only sees the `turn.CloneGate` interface it declares (`internal/turn/execute.go`). `nil` is refused, never treated as a no-op |
| refuse two registered repos from sharing one worktree directory | `internal/repos` | `ValidateWorktreeRoots(existing)` at daemon startup, `ValidateNewWorktreeRoot(reg, existing)` at `repo add`, both before any row is written (AC-PAR-4.8) |
| mark/clear/list a session that still owes the daemon a look | `internal/store` | `store/sessions.go`: `SetSessionNeedsPoll`, `ClearSessionNeedsPoll`, `ListBacklogSessions`; `daemon/tick.go:materialise` retires confirmed closed backlog |
| build a git fixture in a test | `internal/gittest` | `New(tb)` gives a clone + local bare origin + tracking, offline. `AssertUnchanged` / `Capture`+`Snapshot.Diff` prove read-only behaviour |
| call the GitHub API | `internal/github` | Concrete `*Client` backed by go-github/v90; the method inventory is pinned in `internal/github/surface_test.go`. `RateLimitError.RetryAfter` is what the daemon's backoff reads |
| resolve the bot token | `internal/secrets` | `Resolver{Keychain, LookupEnv}` → `Token`. Resolved **once at process start**, never per request |
| strip credential shapes from outbound text | `internal/redact` | `Redactor.Apply(ctx, text, limit) → Body` |
| build a comment body | `internal/report` | Pure rendering; failed-run `agent.RunResult.Diagnostic` is sanitized, literal and bounded |
| resolve bounded conflict regions | `agent.ParseRegionConflictResolution`; `git.CollectConflictSnapshots`, `ValidateConflict` | Identified spans, complete reconstruction, raw digests/statistics and Go syntax/shrinkage checks |
| persist and confirm conflict proposals | `store/conflict_proposals.go`; `turn/conflict_values.go`; `git/worktree_apply.go` | Versioned regions and fresh revision/merge checks; dirty confirmation stays pending with owner-comment recovery guidance |
| recover retained edits before base synchronization | `turn.syncBase`; `harness.DeferredSyncContext` | Dirty normal runs within policy gates use durable/report `deferred_dirty`, with divergence and no sync SHA; both adapters disclose it on every prompt path |
| prepare a reviewable conflict preview | `report.Reporter.PrepareConflictPreview` | Configured redaction, complete metric budgeting and bounded sanitized diff; injected through `turn.Options.ConflictPreviewer` |
| run `claude` or Codex | `internal/harness` / `internal/codex` | Implement `agent.Runner`; each owns its CLI subprocess |
| parse Codex JSONL for runs and structured proposals | `internal/codex/runner.go` | Shared `consume`; private `classifyOutcome` handles process/context evidence |
| add a new coding-agent-tool implementation | `internal/agent`'s `Runner` interface | Zero vendor-naming allowed in any exported identifier (enforced: `purity_test.go`) |
| parse or rewrite plan_brief.md's Story Index or a sub-issue body | `internal/planbrief` | pure text in, struct/rewritten bytes out — no I/O, no git, no forge; `internal/turn` owns the file access |
| classify terminal quota refusal | `agent.Interruption`; `codex/quota.go`, `harness/quota.go` | Neutral reset bound, precision and due time; adapter-owned diagnostic parsing |
| recover an interrupted run | `store.FinishInterruptedRun`, `continuations.go`, `continuation_settlement.go` | Atomic evidence/intent, source-run deduplication, bounded chain and original prerequisite settlement |
| admit or cancel automatic recovery | `daemon/continuation_dispatch.go`, `turn/continuation_dispatch.go` | Saved context, current-intent observation, UTC eligibility and explicit resume |
| replay recovery announcements | `ListPendingInterruptionReports`, `daemon/continuation_cancellations.go`, `report/recovery.go` | Durable notice debt independent of owner watermarks |
| parse a `/schedule` control | `internal/schedule.Parse` | pure parser; caller supplies receipt time and default timezone, and receives a resolved UTC due time or validated dependency delay |
| classify why an attachment was declined | `internal/attach` | `Rejection` / `RejectionReason` (5 reasons); a value, not an error; not routed through `report`'s ReplyKind/Reply |
| tell whether a *stored* run genuinely succeeded (not a live result) | `store.Run.Succeeded()` (`internal/store/runs.go`) | Uses durable semantic outcome when present, preserving interruption across reporting; legacy status/error fallback |
| turn an agent's frontmatter+body into a harness's native output file | `internal/render` | `Renderer`, `Register`/`Lookup`; Claude/OpenCode produce a standalone file plus a root marker section; Codex produces `.codex/skills/<agent>/SKILL.md` |
| install rendered agent artifacts into a target repo, atomically, with provenance | `internal/team` | `Add(root, renders)` validates the batch, writes atomically under a repo flock, and compares recorded hashes with actual file/marker bytes |
| decode/validate the agent team index (teams, shared agents, source/dest) | `internal/team` | `Decode([]byte) (*Index, error)` — strict two-pass JSON, `KeyPattern` for keys/names, faults aggregated |
| compose one agent's section into a shared root file instead of a standalone file | `internal/team`'s `marker.go` | upsert located by agent key alone; an agent shared by multiple teams gets one deduplicated section listing every contributing team |
| classify an installed agent artifact's drift against its current source | `internal/team.Classify` | `Classify` returns InSync/LocallyModified/UpstreamUpdated/Conflict/Missing; caller supplies `teamsource.RenderCurrent`; `LoadLockfile`/`DecodeTeams` expose provenance |
| override which local `.md` file an agent renders from, per repo | `internal/team` | `.litefactory/team.overrides.json`, flat `{agentKey: source}`; `SetOverride`/`UnsetOverride`/`ActiveOverride`/`ReadOverrideSource` (`internal/team/overrides.go`); `team manage <key> <set/unset/show>` is the CLI front door |
| preview native team instructions without installed-tree writes | `teamsource.CaptureRoot` + `render.Native` | Temporary catalog capture validates tiered metadata/skills; native renderer returns in-memory files. Shared contract: `teams/source/skills/workflow/SKILL.md` |
| re-render one agent's current source for one harness, independent of team membership | `teamsource.RenderCurrent(fsys, harnessName, sourcePath)` | Legacy renderer-compatible frontmatter; fresh-render half used by `team.Classify`, with no lockfile/team-union bookkeeping |
| render + collect a team's roster for `team add`, unioning shared-agent membership | `internal/teamsource` | `RenderTeam(root, fsys, teamName, roster, harnessNames)` resolves sources, renders, hashes, and unions `team.ExistingTeams` |
| resolve the team index (embedded default < static-path override < registered-repo override) | `internal/teamsource` | `Resolve` (and `ResolveEmbedded`/`ResolveStaticPath`/`ResolveRegisteredRepo`) → `Resolved{Source, Index, FS}`; every tier loads via `team.Migrate`, never `team.Decode` directly |
| see which `team` CLI subcommands exist | `internal/cli` | all six (`list`/`show`/`add`/`status`/`update`/`manage`) are fully implemented, none a stub — one `Command` constructor each |
| bring installed agent files back in sync with their current source | `internal/cli`'s `runTeamUpdate` (`team update`) | `runTeamUpdate` classifies tracked files, applies UpstreamUpdated/Missing, and reports LocallyModified/Conflict; `--force` overwrites through `team.Add` |
| publish a managed Plan | `turn.PublishPlan`, `planbrief.ParseProposal` | Frozen full-body proposals; factory transport and link-only Story Index |
| enumerate or project acceptance | `planbrief.AcceptanceCriteria`, `acceptance_projection.go` | Complete criterion keys; checkbox updates preserve requirements and unrelated edits |
| route child discussion | `daemon/story_refinements.go`, `store/story_refinements.go` | Parent admission, separate child consumption and bot acknowledgement recovery |
| freeze approved scope | `store.ApproveManagedStories`, `ListApprovedStoryRevisions` | Immutable generation; active workflow version survives replan |
| supply frozen runner context | `agent.ApprovedBundle.Clone`, `turn/approved_context.go` | Cold/resume/fallback parity and read-only completed dependencies |
| ingest progress or recover effects | `turn.StoryProgress`, `store/story_progress.go` | Version-gated evidence ingestion and durable effect debt |
| serialize story effects | `store.LockStoryProgress` | Cross-process parent ownership without a transaction over external I/O |
| finish engineering while children stay open | `store/engineering.go`, `turn/story_progress.go` | Version 2 releases dependencies from snapshot/check evidence; legacy aggregate completion remains in `story_completion.go` |
| assess retained checks without new execution | `agent/validation.go`, `turn/validation.go` | Frozen context, bounded complete criterion/review output and persisted assessments; both runner adapters |
| accept and close an assessed story | `store/acceptance.go`, `turn/acceptance.go` | Durable decision, report, body projection and confirmed closure; historical snapshot retained |
| authorize a reasoned exception | `turn/owner_exceptions.go`, `store/owner_exceptions.go` | Numeric-owner `/waive`, exact immutable expansion and original evidence; grammar in `docs/workflow/owner-exceptions.md` |
| approve a linked follow-up | `turn/followups.go`, `store/followups.go`, `github/followups.go` | Concrete proposal, separate creation/attachment controls and uncertain-response reconciliation |
| deliver accepted work to main | `turn/feature_delivery.go`, `store/delivery.go`, `github/delivery.go` | All-child readiness, retained full reports, explicit PR/head approval, confirmed integration then parent closure; `docs/workflow/delivery.md` |
| do one comment's worth of work | `internal/turn` | `ParseCommand` is the front door; `Execute` is everything after |
| decide *when* work happens | `internal/daemon` | `Tick` dispatches asynchronously; tests use `Daemon.Quiesce` or fixture `settle` to await dispatched turns |
| add a CLI command | `internal/cli` | One constructor in a `cmd_<group>.go` + one line in `Commands()`. No switch, no mutable registry |
| install/remove the LaunchAgent | `internal/launchd` | LaunchAgent in `gui/$UID` only — never a LaunchDaemon (SD-8) |
| build a logger | `internal/logging` | No package-level logger and no `slog.SetDefault`; it is a struct field passed around |
