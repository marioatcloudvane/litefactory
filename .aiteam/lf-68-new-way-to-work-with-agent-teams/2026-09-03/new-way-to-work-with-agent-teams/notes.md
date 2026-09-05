# Implement-mode notes

## 2026-09-05 — Run halted before Phase 1: schema drift judged a severe blocker

Read in full before starting: `plan_brief.md`, `MANIFEST.md`, `US-1.md`, `US-2.md`,
`US-17.md`, `SPIKE.md`. Read `RESEARCH_BRIEF.md` through its fourth revision
pass (the section describing the schema change directly).

**Finding.** `plan_brief.md`'s Shared Decision 1 — which the plan explicitly
tells Implement mode to treat as settled — specifies a flat top-level schema:

```go
Index{SchemaVersion int, Agents map[string]AgentEntry, Shared []string, Teams []TeamEntry}
```

US-1 (the first story, gating all 17 others directly or transitively) builds
exactly this shape: its Architecture Notes quote the same struct verbatim, and
its highest-risk task (T-03, sized L, called out as "the single biggest sizing
risk in this story") is a two-phase strict decoder written against it. US-17's
worked JSON example and `SPIKE.md`'s reference JSON both instantiate the same
flat `agents` map.

`RESEARCH_BRIEF.md`'s fourth revision pass (2026-09-05, ~17:28 — the most
recent accepted design decision, made in direct response to the sponsor's own
comment on issue #68 that a flat top-level `agents` map "will become
complicated" past 20-30 teams) removes that top-level map entirely: `shared`
becomes its own key→`{source,dest}` map, each team's `agents` field becomes a
key→`{source,dest}` map scoped to that team's own non-shared agents, and a new
structural invariant is fixed — an agent key lives in exactly one place in the
whole index (inline under one team, XOR once in `shared`); a key appearing in
more than one place is a loader error, not a merge.

MANIFEST.md's own history entry for that pass independently corroborates that
this is unreconciled: it records, in the project's own words, that
"plan_brief.md, all 18 US-*.md sub-issues (especially US-17.md), and SPIKE.md
all still embed the old flat-map shape... both left for a future Plan
reconciliation pass."

**Judgment.** This is not an ordinary scope ambiguity resolvable on "the most
reasonable reading of the plan" — the two schemas are structurally
incompatible (presence vs. absence of a top-level map; key-string lists vs.
inline full definitions; no uniqueness invariant vs. an exactly-once
invariant), and the incompatibility sits in the one story every other story in
the feature depends on. Two paths were open and both are foreclosed:

1. **Implement the old (plan_brief/US-1) schema as written.** This means
   hand-building the highest-risk piece of the whole feature — a strict
   two-phase decoder — against a shape the sponsor has already rejected on the
   record, on exactly the scaling concern (20-30 teams) this feature exists to
   solve. That is building known-to-be-wrong foundational code and calling it
   delivered.
2. **Implement the new (RESEARCH_BRIEF.md fourth-pass) schema instead**,
   silently substituting it for US-1's spec. That is re-deriving the
   architecture inside Implement mode, which this mode is explicitly not
   authorized to do — reconciling a design document against new research is
   Plan mode's job.

Both are ruled out by the same fact: the frozen artifact (`plan_brief.md`/
`US-1.md`) and the latest accepted design (`RESEARCH_BRIEF.md`) disagree on the
central data structure of the feature, and no reading of either document makes
them fit together. Per the Escalating rubric, this is a **material plan
change**: finishing US-1 (and everything built on it) as currently specified
would contradict the sponsor's own most recent, on-the-record feedback.

**Decision: escalate. No implementation agent was invoked. No code was
written.** The worktree is unchanged from session start except for this note.

See the run's final report (returned to the dispatcher) for the formal
escalation write-up.
