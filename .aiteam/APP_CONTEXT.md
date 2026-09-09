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
meter         → agent              pure normalization, reconciliation, capability diagnostics and exact pricing

redact        →  secrets
team          →  config             write engine, index decode/validate, drift classifier (Status/Classify), per-repo overrides (team.overrides.json)
github        →  naming, redact, secrets
codex         →  agent, config, meter, naming
harness       →  agent, config, meter, naming
report        →  agent, git, naming, redact
repos         →  git, naming, store
turn          →  agent, attach, codex, config, git, github, harness, meter, naming, planbrief, redact, report, store
daemon        →  agent, attach, codex, config, git, github, harness, naming, redact, report, repos, secrets, store, turn
teamsource    →  render, repos, store, team   resolves + renders a team's roster (3-tier index precedence)
cli           →  agent, config, daemon, git, github, identity, launchd, meter, naming, report, repos, secrets, store, team, teamsource
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
| express runner usage | `internal/agent/usage.go` | `TokenUsage`, `UsageObservation`, `UsageInvocation`, `UsageCapture.Emit`; cloned nullable counters and identities feed `turn/meter.go` |
| normalize/reconcile counters | `internal/meter` | `Normalize`, `Validate`, `Add`, `Delta`, `Total`, `Reconcile`; checked arithmetic, subsets, replay identities and baselines |
| qualify provider support | `meter.CapabilityEvidence.Coverage` | Unsupported/missing/complete evidence diagnostics; authentic full-hierarchy certification remains pending |
| collect Claude invocation usage | `harness/usage.go`, `usage_transcripts.go` | Forwarded child stream plus associated root/subagent transcripts; message/model baselines exclude replay, tool links recover ancestry; bounded 2s final drain |
| collect Codex invocation usage | `codex/usage.go`, `usage_rollout.go` | Associated rollouts: prelaunch byte/inode baselines, metadata ancestry, turn-context model and exclusive response records; response-ID/hash dedup; bounded 2s final drain |
| read associated provider sources safely | `harness/usage_transcripts.go`, `codex/usage_rollout.go` | Rooted read-only sources, paged discovery, byte/file limits, nofollow/nonblocking descriptors and replacement checks |
| persist usage and pinned prices | `store/meter.go`, `meter_prices.go`; migrations `0019`, `0020` | `StartMeterAttempt`, `ApplyMeterObservation`, `SetMeterPrices`; durable attempts, evidence, checkpoints, measurements and immutable prices/valuations |
| value token quantities | `meter.CanonicalRate`, `Value` (`meter/pricing.go`) | Exact USD/million, duration fallback and half-even dimension rounding |
| export a coherent usage snapshot | `store.NewMeterSnapshot`, `MeterSnapshot.Next/Close` | Attempt-scoped `MeterFilter`, TEMP projection, bounded pages, current frozen valuations; serializer supplies exact byte accounting |
| inspect metering commands and CSV fields | `cli/cmd_meter.go`, `cmd_meter_prices.go`, `meter_csv.go`; `docs/meter.md` | Summary scopes/filters/details, price list/set and required-project export; reversible spreadsheet text escaping |
| prepare completion metering | `turn/meter_report.go`, `report.MeterSummary.Add` | Stored totals, observed subtotals, legacy reported cost and helper exclusions |
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
| re-render one agent's current source for one harness, independent of team membership | `teamsource.RenderCurrent(fsys, harnessName, sourcePath)` | the fresh-render half `team.Classify` compares against; unlike `RenderTeam`, does no lockfile/team-union bookkeeping |
| render + collect a team's roster for `team add`, unioning shared-agent membership | `internal/teamsource` | `RenderTeam(root, fsys, teamName, roster, harnessNames)` resolves sources, renders, hashes, and unions `team.ExistingTeams` |
| resolve the team index (embedded default < static-path override < registered-repo override) | `internal/teamsource` | `Resolve` (and `ResolveEmbedded`/`ResolveStaticPath`/`ResolveRegisteredRepo`) → `Resolved{Source, Index, FS}`; every tier loads via `team.Migrate`, never `team.Decode` directly |
| see which `team` CLI subcommands exist | `internal/cli` | all six (`list`/`show`/`add`/`status`/`update`/`manage`) are fully implemented, none a stub — one `Command` constructor each |
| bring installed agent files back in sync with their current source | `internal/cli`'s `runTeamUpdate` (`team update`) | `runTeamUpdate` classifies tracked files, applies UpstreamUpdated/Missing, and reports LocallyModified/Conflict; `--force` overwrites through `team.Add` |
| publish a complete managed Plan | `turn.PublishPlan`, `planbrief.ParseProposal` | Full current-base story proposals; `turn/plan_handoff.go` consumes the fixed transient file and persists a link-only index before readiness |
| identify every acceptance obligation | `planbrief.AcceptanceCriteria` | Shared publication/evidence parser; enumerates explicit, numbered and prose criteria without dropping unnamed obligations |
| route managed child discussion | `daemon/story_refinements.go`, `store/story_refinements.go` | Closed-child discovery, canonical parent admission, separate child consumption and bot-verified acknowledgement recovery |
| freeze and reload an approved generation | `store.ApproveManagedStories`, `ListApprovedStoryRevisions`, `turn/managed_scope.go` | Atomic immutable revisions, pending-refinement exclusion and durable notices; explicit quiescent parent `/plan` or `replan` opens unfinished scope |
| supply approved stories to either runner | `agent.ApprovedBundle.Clone`, `harness/stories.go`, `turn/approved_context.go` | Complete JSON context on cold/resume/fallback, relationship checks, read-only completed dependencies and aggregate-only parent recovery |
| observe and close a story during its parent run | `turn.StoryProgress` | Tick-polled strict event validation, immutable snapshot evidence and durable ordered completion/invalidation effects |
| serialize story effects across processes | `store.LockStoryProgress`, `config.Paths.StoryProgressLocksDir` | Nonblocking parent ownership covers ingestion and external delivery without holding a SQL transaction |
| distinguish story closure from final feature delivery | `store/story_completion.go` | Children close at scoped verification; feature completion additionally records aggregate/save success and delivered effects |
| do one comment's worth of work | `internal/turn` | `ParseCommand` is the front door; `Execute` is everything after |
| dispatch, diagnose and join polling workers | `daemon/tick.go`, `daemon/worker.go` | `Tick` admission sources and worker/forge counter aliases; `runWorker` lifetime diagnostics and executor-request count; `Quiesce` joins cleanup and diagnostics |
| add a CLI command | `internal/cli` | One constructor in a `cmd_<group>.go` + one line in `Commands()`. No switch, no mutable registry |
| install/remove the LaunchAgent | `internal/launchd` | LaunchAgent in `gui/$UID` only — never a LaunchDaemon (SD-8) |
| build a logger | `internal/logging` | No package-level logger and no `slog.SetDefault`; it is a struct field passed around |
