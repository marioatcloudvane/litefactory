# Plan brief: agent-originated story-creation proposals

- Feature: expose the creation proposer so owners can create stories from a Research run
- Branch: lf/313-expose-the-creation-proposer-so-owners
- Date: 2026-09-19
- Parent issue: #313
- Citation base commit: e725113 (branch merged with origin/main; the graph Research-node code is present)

## Shared Decisions

These bind more than one story. Engineers do not re-litigate them per task.

- **Tool identity.** One new family tool named `issues_create_prepare`, effect label `input`, Research mode only, native MCP only. It needs no grant because it retains a proposal and publishes nothing.
- **Installation is not a grant.** Installing the tool never registers `issues_create_batch`, never sets the creation-grant flag and never binds a grant. Presence in the installed registrations is the story-creation gate; the family-allowed rule checks mode only.
- **Catalog version.** The family catalog version moves from `families-v2` to `families-v3`. It is part of every attempt's permission digest, so one pinned test changes deliberately.
- **Atomic retention seam (D-1).** The store splits retention into an unexported transactional helper plus two exported entry points: the existing one (unchanged) and a new one that also upserts the per-run bookkeeping row in the same transaction. The preparer receives its retain function by injection; redaction, marker, bounds and canonical envelope are reused, never re-implemented.
- **Session is derived, not supplied (D-2).** The bookkeeping row resolves its session from the run in SQL and refuses when that session's task differs from the source task.
- **One shared auto-link helper (D-3).** The rule "append the source issue to every draft's links, once, at most 16 targets" is extracted once and used by both the hand-written owner path and the new tool.
- **Own delivery identity (D-4).** The presentation comment uses a `RunID == 0` delivery with purpose `creation-proposal:<hash>` on the run's trigger. `RunID` must stay 0, otherwise completion re-finalizes the run and overwrites its result comment. It must not reuse the reply path, which skips when the trigger is already consumed.
- **Legacy-runless probe carve-out (D-5).** The runless-delivery ambiguity probe is skipped for the `creation-proposal:` purpose only. This is the single change to shared delivery code and has its own regression test.
- **Receipt projection under two keys (D-6).** The receipt projection is registered under both `creation_proposal` and `tool:issues_create_prepare`.
- **Daemon hook keys off durable bookkeeping (D-7).** Whether a presentation is owed is decided by the bookkeeping row (absent or presented means nothing owed), not by a second configuration check.
- **Optional reporter capability (D-8).** The daemon reaches the new reporter method through an optional capability assertion and fails loudly, never silently, when a row exists but the capability is missing.
- **Nonce and cap.** Nonce is derived from task, run, attempt and the tool's exact argument bytes; the 48000-byte cap applies at call time; over-size is a refusal, never a truncation.
- **Advice only.** The split recommendation appears in the tool description, the prompt guidance and the agent's report. It has no threshold, no enforcement and never blocks a proposal.
- **Criterion numbering.** Criterion ids are global across the four stories and follow the feature spec (`AC-001` to `AC-028`), with new ids from `AC-029` upward.

## Workflow

Shipping order: S1 (bookkeeping) first. S2 (tool) and S3 (presentation delivery) each depend only on S1 and may proceed in parallel. S4 (post-run hook and recovery) depends on S3; its end-to-end criteria also want S2 merged, but it can be built and tested against a hand-seeded bookkeeping row.

```
S1 -> S2
S1 -> S3 -> S4      (S4 end-to-end also wants S2)
```

Hard couplings: S2 and S4 both rely on the S1 row; S3 and S4 share the delivery identity from D-4 and the carve-out from D-5.

## Cross-Story Constraints

- Native MCP only for the new tool; the hand-written `/mcp-create prepare` path, approval syntax, 30-minute window, quotas and `issues_create_batch` publication are unchanged.
- The new tool must be registered for both the legacy Research path and the graph Research-node path. The existing graph-path exclusions for the comment service and the creation grant stay as they are.
- Registration happens before the family permission digest is computed and before the lease is bound, and the installed catalog must still equal the launch spec's permitted tools.
- No new package, no new module dependency, no new configuration key. The gate is the existing story-creation setting.
- No transaction may span a forge call. Ordering is render, durable intent, post, confirm, complete, and only then mark presented.
- Tests are offline Go tests with no build tags, use real migrated SQLite where the store is involved and hand-written fakes elsewhere, and use injected clocks rather than sleeps.
- Evidence limits (see below) apply to every story: live host invocation and real forge-outage behaviour are unverified obligations and must not be claimed as demonstrated.
- Every story's definition of done includes `go build ./...` and `make check`.

### Evidence inventory (shared)

`docs/decisions/nested-agent-evidence.md` does not exist in this worktree, so obligations are read from repository convention: offline Go tests, no integration tags.

| Criteria | Proof method | Source availability | Version bounds | Missing prerequisite |
|---|---|---|---|---|
| AC-001 to AC-006 | turn and submission unit tests over catalog composition and guidance text with a fake native-MCP runner | in-repo, offline | families-v3 | none |
| AC-007 to AC-015 | in-process dispatcher tests over real migrated SQLite with a session row | in-repo, offline | families-v3, migration 0055 | none |
| AC-016 to AC-024 | turn and daemon tests with fake forge and reporter fixtures, injected tick clock | in-repo, offline | migration 0055 | none |
| Live host invocation of the tool | none available | nested-agent observation is unsupported in this sandbox | not applicable | authentic external capture from a real Claude or Codex Research run; unsatisfied obligation, not dropped scope |
| Real forge outage produces the retry path | fake forge returning errors proves factory behaviour only | fake only | not applicable | observation against a real GitHub outage; unsatisfied obligation |

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| bookkeeping | Per-run proposal bookkeeping | [#352](https://github.com/marioatcloudvane/litefactory/issues/352) |
| tool | The issues_create_prepare Research tool | [#353](https://github.com/marioatcloudvane/litefactory/issues/353) |
| presentation-delivery | Presentation comment delivery identity | [#354](https://github.com/marioatcloudvane/litefactory/issues/354) |
| presentation-hook | Post-run presentation hook and retry | [#355](https://github.com/marioatcloudvane/litefactory/issues/355) |

## Out of Scope

- Mode labels on created issues; the batch format is unchanged.
- Approval launching a run; approval still records intent only and the owner starts the next Research run within 30 minutes.
- Changing the 30-minute window, quotas or approval syntax.
- Removing or changing the hand-written prepare path.
- Any change to `issues_create_batch` publication, reservation or reconciliation.
- Issue #314 queue wedging. The carve-out in D-5 and the deferral rule touch the same delivery slot, but this feature does not fix #314 and its tests must not depend on the wedge.
- Automatic splitting, split thresholds or any enforcement of the split advice.
- Instruction files under the protected trees; the advice ships in Go text, so no protected-tree story split applies.
- Presenting proposals from non-Research modes.

## Provenance

- Citation base: commit e725113. Line citations in the stories are indicative; re-pin against the working tree when implementing.
- Migration number `0055` is free at this base; a concurrent feature taking it first is a rename, not a redesign.
- Known-stale areas: the pinned digest test in the turn package must be updated deliberately for `families-v3`.
- Assumptions recorded (no questions were permitted): the tool name; the mode-only family-allowed rule; parents limited to what the existing canonical validator permits (linked means no parent, attach means the source issue); presentation intent is minted only when no other unresolved delivery exists on the trigger, otherwise deferred to the next tick; the daemon hook ignores a configuration flip after retention.
- Supporting session files: RESEARCH_BRIEF.md, RESEARCH_TECHNICAL.md, feature-spec.md, ARCH_REVIEW.md in this session directory.
