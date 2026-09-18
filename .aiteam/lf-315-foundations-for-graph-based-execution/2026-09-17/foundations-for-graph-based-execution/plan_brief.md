# Plan Brief: Foundations for graph-based execution (#315, first plan scope)

- Feature: foundations-for-graph-based-execution
- Branch: lf/315-foundations-for-graph-based-execution
- Date: 2026-09-18
- Parent issue: #315 ("Foundations for graph-based execution", split from #230's roadmap)
- Citation base: HEAD of `lf/315-foundations-for-graph-based-execution` at time of this Plan run (post-#259; `docs/` deleted on this branch)

## Shared Decisions

Binding on two or more of the five stories below. Story-specific architecture
notes carry anything true of only one story; it is not repeated here.

- **D1 — model tier stays vendor-neutral (closes "Fable").** Node-level model
  selection is the DOT attribute `lf_model_tier`, restricted to the existing
  closed `config.Tier` vocabulary (`flagship|balanced|fast`), validated at
  parse time in `internal/graphexec` against `config.Tier.Valid()`. No literal
  model slug, no new tier. Resolution to an actual model string is unchanged —
  it stays `Tiers.ResolveModel(Kind, Tier)` at execution time
  (`internal/turn/delivery.go:69`), which no story in this scope touches.
- **D2 — DOT/markdown path convention, reconciled.** Owner convention:
  `<catalog-root>/<team>/<mode>.dot` and `<catalog-root>/<team>/<mode>/<agent>.md`.
  `<team>` is the team id (`go-team` today); `source` in `teams/source/` is a
  catalog *channel/root* (editable vs. released vs. installed), not a team
  name — `teams/source/index.json` is already multi-team-shaped and
  team-namespaces its agents, so reading `source` as a team breaks at exactly
  the plural-team case the owner is designing for. Concretely today:
  `teams/source/go-team/plan.dot`, `teams/source/go-team/plan/go-architect.md`.
  **This scope commits only to storing such a path as an opaque, syntactically
  validated string alongside its sha256 digest.** No story resolves a root,
  reads a `.dot` file from disk, or restructures `teams/source/agents/**`.
- **Attempt identity is `(instance_id, node, attempt)` from the first
  migration**, not retrofitted later, per the lesson of migration
  `0047_submission_check_snapshot_scope.sql`. `attempt` counts *every*
  dispatch of a node — a retry after failure and a legitimate bounded-loop
  re-entry are the same counter, bounded by the node's `lf_limit`. Backoff
  (S4) is therefore keyed on the count of *consecutive failed* attempts, a
  derived value distinct from `attempt` itself.
- **Attempt completion is one composite accessor, one transaction.**
  `StartGraphNodeAttempt` (tx) reserves identity; the activity dispatch itself
  happens with **no transaction open**; `CompleteGraphNodeAttempt` (tx)
  finishes the attempt, records the output, appends the transition, and
  advances `graph_instances.current_node`/`status` — all or none. This is what
  makes the reopen-and-resume criterion (S5) provable at all.
- **A failed-but-conducted activity is a result, not an error.**
  `ActivityResult` carries `Outcome`, `Kind`, `Output`, and `Diagnostic`. A
  handler returns Go `error` only when the activity could not be conducted at
  all (unresolvable dependency, cancelled context) — never to report an
  ordinary failure outcome.
- **`InvokeAgentConfig` lives in `internal/graphexec`**, not `internal/turn` —
  D1 requires the tier to be a parse-time validation fault, and `graphexec` is
  the only package that validates at parse time. It ships as a decoded type
  only; no handler exists in this scope.
- **Inertness is a deliverable, under test, not an omission.** Nothing in this
  scope is reachable from `Daemon.Tick`, `turn.Execute`, or any CLI command.
  Each of S3 and S4 carries a structural test proving its new entry point has
  no production caller yet.
- **Package-map additions:** exactly two new edges — `graphexec → agent,
  config` and `turn → graphexec`. `daemon` does **not** import `graphexec`;
  the poller trades only in `store` rows and plain `int`s. `.aiteam/APP_CONTEXT.md`'s
  package map is updated in the same change as S1.
- **Path-shaped values are refused syntactically at entry.**
  `graph_definitions.path` and the `lf_agent_markdown` attribute must be
  non-empty, relative, slash-separated, and free of `.`/`..` segments,
  backslashes, and a leading `/`. Rooted resolution, symlink refusal, and
  digest re-verification stay with whoever resolves the path later (#316) —
  this scope stores/decodes only, never opens.

## Workflow

Shipping order (dependency graph, not task-level ordering — that is each
story's own tasks section):

1. **S1** (`internal/graphexec` core) and **S2** (`internal/store` persistence)
   proceed **in parallel** — S2 imports no `graphexec` type and needs no new
   dependency, so it is not blocked by S1's Gonum risk (see Cross-Story
   Constraints).
2. **S3** (`internal/turn` activity layer) depends on both S1 and S2.
3. **S4** (`internal/daemon` poller skeleton) depends only on S2 (it does not
   import `graphexec`).
4. **S5** (composition integration test) depends on S1, S2, and S3.

## Cross-Story Constraints

- **Platform:** all new code lands under `internal/`; no `pkg/`, no new
  `cmd/`; no HTTP/gRPC/CLI surface is added anywhere in this scope.
- **Concurrency:** no goroutine is created anywhere in this scope; every store
  accessor and `ExecuteNodeAction`/`ActivityHandler.Execute` take `ctx` first;
  no SQL transaction is ever open while an activity handler dispatches.
- **Security:** SEC-20/SEC-21 (path selection) are addressed by the
  syntactic-refusal rule above; no secret, credential, or forge call is
  touched by any story in this scope.
- **Test invariants:** table-driven stdlib `testing`, hand-written fakes (no
  mocking framework), real SQLite for anything touching `store` (TEST-6), and
  a structural (`go/parser`) test wherever a new package or entry point must
  be proven pure/inert.
- **Ring-fenced areas:** no story touches `.claude/**` or `.codex/**`; no
  existing migration file is edited; no existing Go file's bytes change
  except `go.mod`/`go.sum`.
- **Carried risk (not a blocker to freeze, flagged for implementation):**
  `gonum.org/v1/gonum v0.16.0` is confirmed absent from `go.mod`, `go.sum`,
  *and* the local module cache used to write this plan. Adding it (S1's first
  task) requires module-proxy/network access in the implementation sandbox.
  If unavailable, S1 blocks at its first task and S3/S5 block transitively;
  S2 and S4 are unaffected. This is an infrastructure contingency for the
  owner, not a scope or design question — see the completion report's Open
  Questions.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| S315-graphexec-core | Parse, validate and navigate workflow graphs | [#330](https://github.com/marioatcloudvane/litefactory/issues/330) |
| S315-store-graph-persistence | Persist graph definitions, instances and execution history | [#331](https://github.com/marioatcloudvane/litefactory/issues/331) |
| S315-turn-activity-layer | Dispatch generic activities through a bounded execution step | [#332](https://github.com/marioatcloudvane/litefactory/issues/332) |
| S315-daemon-poller-skeleton | Provide unwired graph scheduling and retry helpers | [#333](https://github.com/marioatcloudvane/litefactory/issues/333) |
| S315-graph-execution-composition | Prove graph execution composition against real persistence | [#334](https://github.com/marioatcloudvane/litefactory/issues/334) |

## Out of Scope

- Shipping any real `.dot` file (production or example) — plausibly #316's
  `research.dot`.
- Restructuring `teams/source/agents/**` to a mode-namespaced layout — #317's
  job.
- Reading a `.dot` file from any root (worktree or installed catalog) and
  deciding which root is authoritative.
- A real `invoke_agent` activity handler, any agent invocation, any
  `harness`/`codex` call, session-resume behavior.
- Wiring anything into `Daemon.Tick`, `daemon/executor.go`, `turn.Execute`, or
  any CLI command.
- Old/new execution-path compatibility **admission** (#315's own scope item
  5) beyond naming the precedent to reuse — there is no second live path to
  admit into yet. Carried as remaining #315 capability debt, not dropped.
- Token/time baseline instrumentation (#315's own scope item 6, second half).
  Same: remaining #315 debt for a later Plan increment.
- Any change to `config.Tier`, `defaultModelByTier`, `AGENT_TIERS.md`, or
  `.claude/agents/**` frontmatter.

## Provenance

- Grounded in `$session/RESEARCH_BRIEF.md` (Research runs 0001-0005) and this
  run's fresh re-verification of the same claims against current source
  (`go.mod`, `internal/config/tier.go`, `internal/turn/delivery.go`,
  `internal/store/migrations/`, `internal/store/migrate.go`, `teams/`,
  `teams/source/`, `internal/teamsource`, `internal/daemon/backoff.go`,
  `internal/planbrief/typed_decode.go`).
- Known-stale areas found this run: `docs/` (including
  `docs/decisions/nested-agent-evidence.md` and `docs/agent-result-contract.md`)
  no longer exists on this branch, though `.aiteam/APP_CONTEXT.md:64` still
  links the former. Not repaired by this scope; recorded as evidence
  obligation EO-5 in `$session/feature-spec.md` and mitigated because no
  criterion in this scope asserts coding-agent runtime behavior — every
  criterion is provable by a Go test that actually runs.
- Re-pinning requirement: the Gonum decoder attribute-drop claim (EO-1) is
  unverified as of this Plan run (module unreachable in this sandbox); the
  obligation is discharged by S1's own round-trip test against the pinned
  `v0.16.0`, not by this plan asserting the claim as settled fact. A future
  version bump of Gonum re-opens EO-1.

## Publication continuation — run 0008

The owner explicitly requested publication of the reviewed five-story, 28-task first increment. The complete typed proposal preserves that scope and the saved criterion identities. Tests using real SQLite are assigned to the integration-test engineer; structural and pure tests remain unit-test work. The proposed migration number 0048 is provisional: use the next available number at implementation time. Factory publication and save establish readiness; local artifacts do not.

Publication identifiers use `S315-*` story keys and `T-001-1`-style task IDs because the shared result format requires a leading letter and forbids dots. These map directly to the reviewed `315-*` keys and `T-001.1`-style task IDs; no typed story identities existed in the supplied result baseline. Scope and dependencies are unchanged.
