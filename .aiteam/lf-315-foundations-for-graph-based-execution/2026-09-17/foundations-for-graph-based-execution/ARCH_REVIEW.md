# Architect: Go Architect
# Created: 2026-09-18
# Feature: #315 Foundations for graph-based execution — first plan scope
# Spec reviewed: $session/feature-spec.md (Status: APPROVED, 2026-09-18)

**Verdict: APPROVED WITH BINDING CORRECTIONS.** The layering is right, the
inertness discipline is right, and both owner decisions this run (D1 closed tier
vocabulary, D2 catalog-root-relative paths) hold architecturally. Seven
corrections below are binding on story authoring; one owner decision (DEV-2) is
carried alongside, not waited on.

---

## System Overview

### How This Feature Fits

Today the package map (`.aiteam/APP_CONTEXT.md:24-47`) has one execution spine:
`daemon` discovers work and owns time, `turn` does one comment's worth of work,
`store` persists it, and `harness`/`codex` run the subprocess. Graph execution
adds a *second, parallel, currently-dead* spine at the same three levels:

```
graphexec  → agent, config                  NEW leaf-adjacent pure core
store      → (leaf)                         + 5 graph_* tables, + accessors
turn       → … + graphexec                  + ActivityRegistry, ExecuteNodeAction
daemon     → … (store only, NOT graphexec)  + poller skeleton, unwired
```

`internal/graphexec` sits one level below `turn`, in the same purity class as
`internal/planbrief` and `internal/naming`: bytes and structs in, structs and
faults out. It is the only new package. It answers three questions — *what does
this profile say*, *is it internally consistent*, *given a node and an outcome,
which node is next* — and nothing else.

The four layers touch exactly three existing tables (`sessions`, `runs` as FK
targets; `schema_migrations` implicitly) and change no existing Go file except
`go.mod`/`go.sum`. That "nothing is wired" property is load-bearing: it is what
lets #316–#321 wire real modes onto proven machinery, and it is what keeps this
scope from being able to break a live daemon. Treat inertness as a deliverable
under test (EO-4), not as an omission.

### New Packages / Components

| Component | Responsibility (one sentence) |
|---|---|
| `internal/graphexec` | Decode DOT bytes into a typed `Profile`, validate it against an action `Registry` with aggregated faults, and decide the next node from a declared outcome — with no clock, no filesystem, no database. |
| `internal/graphexec/dot.go` (file, not package) | The single file permitted to import Gonum; converts Gonum's decoded graph into this package's own types and is never depended on by the rest of the package's semantics. |
| `internal/store` graph accessors | Durably record a pinned definition, a running instance, per-node attempts, their outputs, and an append-only transition trace; refuse every identity and immutability violation as a typed sentinel. |
| `internal/turn` activity layer | Map a node's action name to a registered `ActivityHandler`, dispatch exactly one activity outside any transaction, and record the whole outcome atomically afterwards. |
| `internal/daemon` graph poller | Two pure-ish functions — select due instances, compute the next attempt instant — that own time and backoff for graphs, called by nobody in this scope. |

### Key Boundaries

All boundaries in this scope are in-process Go calls. There is no HTTP, gRPC,
channel, event bus or subprocess anywhere in #315.

| Boundary | What crosses | How |
|---|---|---|
| `graphexec` → `turn` | `Profile`, `Node`, `ActionSpec`, `Decide`'s target name | Direct import; `turn` owns the instance of `Registry` and passes it down. |
| `graphexec` ↔ `store` | **Nothing.** Correlated by `(team, mode, digest)` values only | The spec is right: `store` holds no `graphexec` type and `graphexec` holds no `store` type. Preserve this — it is what keeps the core testable with zero fixtures. |
| `turn` → `store` | `GraphDefinition`, `GraphInstance`, `GraphNodeAttempt`, `GraphOutput`, `GraphTransition` values | Accessors, `ctx` first (CONC-5). |
| `turn` → `ActivityHandler` | `ActivityRequest` (cloned attrs) in, `ActivityResult` out | Registry lookup, never a switch — the `RunnerSet` rule (`internal/turn/execute.go:33-43`). |
| `daemon` → `store` | `[]store.GraphInstance` | Accessor only. **`daemon` does not import `graphexec` in this scope** — see Decision 4. |
| DOT bytes → `graphexec` | `[]byte` | `ParseProfile([]byte)`; no file access. Who reads the file is OQ-2's problem, correctly deferred. |

### Data Flow

Primary (a single node executes):

```
poller selects due instance (daemon, holds `now`)
  -> caller loads Profile bytes + Registry (out of scope: #316)
  -> store.StartGraphNodeAttempt      [tx 1: reserve identity, status=running]
  -> ActivityRegistry.Resolve(node.Action) -> handler.Execute(...)   [NO transaction open]
  -> graphexec.Decide(profile, node, result.Outcome)
  -> store.CompleteGraphNodeAttempt   [tx 2: finish attempt + output + transition + current_node]
```

Terminal node: no dispatch, no attempt row, instance status → `succeeded` in one
write.

Definition pinning (once per `(team, mode, digest)`):

```
bytes -> sha256 digest -> store.CreateGraphDefinition(team, mode, profile_version, path, digest)
       -> existing row returned if the triple already exists
```

### Concurrency & Context

- **No goroutine is created anywhere in this scope.** CONC-2's budget is
  unchanged, and that must stay true: `ExecuteNodeAction` runs on its caller's
  goroutine, the poller on its caller's. If any story finds itself wanting a
  goroutine, that is a design change, not an implementation detail.
- The `graphexec.Registry` and every `Profile` derived from it are built once and
  read by up to `max_concurrent_runs` workers, exactly like `RunnerSet`. They are
  therefore **build-then-freeze, immutable after construction, no mutex** — and
  because they are shared, CONC-1 applies at every struct boundary they cross
  (Decision 3).
- Time enters at exactly two places, both as parameters: `pollDueGraphInstances(…,
  now, …)` and `nextGraphAttemptAt(…, last, …)`. Neither reads a clock. This
  keeps `daemon/clock_test.go`'s "only `Tick` reads the clock" invariant true
  without amending it — and TEST-10 forbids amending it to pass.
- `context.Context` is the first parameter of every store accessor and of
  `ExecuteNodeAction` and `ActivityHandler.Execute` (CONC-5). It is never a
  struct field, and `nextGraphAttemptAt` holds no `context.CancelFunc`.
- **No SQL transaction may be open across the handler dispatch** (CONC-18). This
  is the single most important structural rule in S3.

### Shared State and Side Effects

- The only durable side effects are rows in five new tables. No file is written,
  no forge call is made, no git command runs, no subprocess starts.
- `graph_outputs.payload` will eventually hold agent-authored bytes. Nothing in
  this scope renders them, but the moment something does, it goes through
  `redact.Redactor.Apply` at the posting boundary (SEC-6), and the stored copy
  stays unredacted like `$LF_HOME/logs`. Record that in `store`'s column comment
  now so the next reader does not have to rediscover it.
- `graph_definitions.path` and the `lf_agent_markdown` attribute are
  **path-selecting values sourced from a file**. #315 stores and decodes them
  only, but they are the seam SEC-20/SEC-21 care about (Decision 6).
- Migration `0048` reserves a schema version number. If another branch lands a
  0048 before this one merges, the file renumbers; the number is not identity.

---

## Binding decisions

These bind more than one story. Story authoring must reflect them; they are not
suggestions to the implementer.

### Decision 1 — Gonum is contained to one file, pinned by a purity test

Adding `gonum.org/v1/gonum` makes a fifth direct require where TEST-2 says the
module "SHOULD keep that shape", and platform guidance says to keep `internal/`
core logic's third-party surface shallow. Issue #315 asks for Gonum-based DOT
parsing, so the deviation is taken (DEV-1) — but it is **contained**, using the
template this repo already owns:

`internal/config/purity_test.go` permits exactly one file (`config.go`) to import
exactly one third-party package (`gopkg.in/yaml.v3`) and proves it with
`go/parser` over the package directory. `internal/graphexec` gets the identical
test with `dot.go` / `gonum.org/v1/gonum`. Everything else in `graphexec` —
`Validate`, `Decide`, the registry, attribute decoding — is stdlib-only and keeps
working if Gonum is ever swapped out. This is what makes DEV-2's contingency a
one-file change instead of a story rewrite.

### Decision 2 — Attempt completion is one accessor, one transaction; dispatch is outside it

The spec's "one transaction each" per accessor is not sufficient. Four sequential
accessors (finish attempt, record output, record transition, advance
`current_node`) leave four crash windows, and S5's "reopen the database mid-run
and resume from the persisted `current_node` with no duplicate attempt row" is
unprovable against them.

Binding shape:

- `StartGraphNodeAttempt` — one transaction (`BEGIN IMMEDIATE`, the
  read-then-write rule in `store/store.go:38`): assert no `running` attempt for
  `(instance, node)`, allocate `attempt`, insert `status='running'`.
- **Dispatch happens with no transaction open** (CONC-18).
- `CompleteGraphNodeAttempt(ctx, …)` — one transaction: finish the attempt row,
  insert any `graph_outputs` row, insert the `graph_transitions` row, and set
  `graph_instances.current_node`/`status`/`next_due_at`. All or none.

`RecordGraphOutput` / `RecordGraphTransition` may still exist as standalone
accessors for tests and for the trace, but `ExecuteNodeAction` must use the
composite. This is CONC-4's "durable before announcement" applied one level down:
the transition is the announcement.

### Decision 3 — Shared immutable values are cloned at every struct boundary (CONC-1)

`Node.Attrs map[string]string`, `Profile.Nodes []Node`, `Profile.Edges []Edge`,
`ActionSpec.Outcomes []string` and `ActivityResult.Output []byte` all cross struct
boundaries into concurrent turns. `Registry.Register` clones `ActionSpec.Outcomes`
on the way in; `Registry.Resolve` clones on the way out; `ExecuteNodeAction`
clones `Node.Attrs` into `ActivityRequest` so a handler cannot mutate a shared
profile. `maps.Clone`/`slices.Clone` are enough; this is CONC-1, not defensive
style.

### Decision 4 — `daemon → graphexec` is rejected; the poller trades in store rows and ints

The spec offers `daemon → graphexec (types only, if needed)`. It is not needed and
should not be added. `pollDueGraphInstances` returns `[]store.GraphInstance`, and
`nextGraphAttemptAt(attempt int, last time.Time, policy retryPolicy)` takes the
node's `lf_limit` as a plain `int` inside `retryPolicy`. Keeping `daemon` ignorant
of graph *semantics* is the same separation that keeps `daemon` ignorant of what a
comment means: `daemon` owns when, `turn` owns what.

Confirmed new edges are therefore exactly two: `graphexec → agent, config` and
`turn → graphexec`. Both are sound. `config` is a documented leaf every package
may import and `config/purity_test.go` pins it as such; `agent → naming` only, and
`agent.Mode` is the closed vocabulary a profile must speak. `turn` already imports
`agent`, `config` and `store`, so `turn → graphexec` introduces no cycle and no
new transitive reach. **`.aiteam/APP_CONTEXT.md`'s package map must be updated in
the same change** (a task, not an afterthought).

### Decision 5 — `(instance_id, node, attempt)` is sufficient; `attempt` counts every dispatch

The 0047 analogy does not transfer, and the reason is worth writing down so it is
not re-litigated. 0047 widened `(run_id, reference)` because a `run_id` did *not*
imply the snapshot that produced a check — two snapshots could share one run. Here
`instance_id` **does** imply `definition_id`, which implies `digest`: an instance
is pinned to one definition for its whole life (the spec's own edge case:
"instances stay pinned to the digest they started on"). A third axis would be
derivable from the second and would encode a mutability that the schema forbids.
No `snapshot_ref` column. Add one if and only if an instance ever becomes
re-pinnable, which is a different feature.

The genuine ambiguity is what `attempt` counts, and all four stories depend on the
answer. **Binding: `attempt` is a per-`(instance, node)` monotone counter over
*every* dispatch — a retry after failure and a legitimate second pass of a bounded
refine loop are the same counter.** `lf_limit` bounds that counter. Consequences
story authors must carry:

- S1: `lf_limit`'s documented meaning is "maximum dispatches of this node per
  instance", not "maximum retries".
- S4: "backoff grows monotonically per *consecutive failed* attempt" is therefore
  **not** `attempt`. It is derived from the trailing run of `failed` attempt rows
  for `(instance, node)`; a successful loop re-entry resets it. `retryPolicy` must
  carry that consecutive-failure count as a separate input from `attempt`.
- If retries and loop passes ever need to be told apart, that is a new `pass`
  column, not a reinterpretation of `attempt`.

### Decision 6 — Path-shaped values are syntactically refused at entry

`graph_definitions.path` and the `lf_agent_markdown` attribute both select a file
that something downstream will open. #315 is the only cheap moment to constrain
them. SEC-20 ("story text MUST NOT select paths") and SEC-21 (rooted access with
symlink-component refusal) are about exactly this class of value.

Binding, and deliberately *syntactic only*: a stored/decoded path reference must
be non-empty, relative, slash-separated, and free of `.` / `..` segments,
backslashes, leading `/` and drive-letter prefixes. `graphexec` refuses a bad
`lf_agent_markdown` as a validation fault; `store.CreateGraphDefinition` refuses a
bad `path` as a typed error. **Rooted resolution, `Lstat` symlink refusal and
digest re-verification stay with whoever resolves the path** (OQ-2, #316) — that
is `teamsource.CaptureRoot`'s existing discipline and must not be reimplemented
here. This does not resolve OQ-2; it makes every answer to OQ-2 safe.

### Decision 7 — `InvokeAgentConfig` lives in `graphexec`, not `turn`

The spec contradicts itself: the API table places `InvokeAgentConfig` under
`internal/turn`, while S1's acceptance criteria and the D1 business rule ("validated
at parse time") place it in `graphexec`. **It lives in `graphexec`**, in its own
file (`invoke_agent.go`), because D1 requires the tier to be a parse-time
validation fault and because S1's criteria test it. `turn` does not redeclare it;
a future real handler imports it. Story authoring must correct the API table's row
or two stories will build the same type.

Related, and a smell worth bounding now: `lf_model_tier` and `lf_agent_markdown`
are action-specific knowledge inside a package whose stated virtue is being
registry-driven. Acceptable for two attributes, documented in `doc.go` as *the*
known exception. **Tipping point: a third reserved attribute moves this to a
declarative attribute schema on `ActionSpec`.** State the tipping point in the
comment so the third one triggers the refactor instead of the pattern.

### Decision 8 — One migration file, and the indexes the spec omits

One file, `0048_graph_execution_foundations.sql`, covering all five tables.
Precedent is unambiguous: `0019_*.sql` creates six tables and four indexes in one
file; `0042_*.sql` creates two. Splitting buys nothing — `migrate.go` orders by
numeric prefix and each file is one transaction, so five files would mean five
partially-migratable states of one indivisible feature.

The spec tables no indexes at all. SQLite gives an index for `UNIQUE` and for
`INTEGER PRIMARY KEY` and nothing else, so as specified every FK-following read
and the poller's hot query are table scans. Required:

| Index | Why |
|---|---|
| `graph_instances(next_due_at, id)` | The poller's only query. Leading column is what it filters on; `id` makes the order deterministic (TEST-7). |
| `graph_node_attempts(instance_id, node, attempt)` UNIQUE | Identity (Decision 5) and the "is one running" check in the same index. |
| `graph_transitions(instance_id, id)` | `ListGraphTransitions` in insertion order, per instance. |
| `graph_outputs(node_attempt_id, id)` | FK following; SQLite does not index a child FK column for you. |
| `graph_definitions(team, mode, digest)` UNIQUE | Already implied by the spec's constraint; state it as the constraint, not a separate index. |

`team_snapshot_digest` stays **nullable** (OQ-3 option b). #315 never captures a
`teamsource.Snapshot`, so NOT NULL would force a fabricated value, and the
requirement it encodes ("graph and roster pinned together") is a write-path
invariant that whoever wires a real mode enforces in Go. Tightening it later costs
nothing — it is an application check, not the 12-step SQLite table rebuild that a
NOT NULL/CHECK addition would be.

Storage conventions this repo already fixes, so they are not open choices:
timestamps are UTC RFC 3339 with millisecond precision via `store.FormatTime`,
zero time means NULL, and `run_id`'s zero means NULL (`store/store.go:44-60`).
`foreign_keys=ON` is a DSN pragma, so every `REFERENCES` clause here is enforced,
not decorative — order the `CREATE TABLE` statements so parents precede children.

### Decision 9 — CONC-3 at the activity boundary: a failed activity is a result

`ActivityResult{Outcome, Kind, Output}` as specified cannot satisfy S3's own
criterion "a fake handler returning an error records `failed` with the handler's
diagnostic preserved" — there is no field to preserve it in. And CONC-3 is
explicit: "an attempted operation that failed MUST return a result describing its
outcome; errors are reserved for operations that could not be conducted or
recorded."

Binding split:

- A handler that **ran something that failed** returns an `ActivityResult` whose
  `Outcome` is one of the node's declared failure outcomes, plus a new
  `Diagnostic string` field. That is a normal transition, not an error.
- A handler returns `error` only when the activity **could not be conducted** —
  unresolvable dependency, cancelled context, a write that could not be recorded.
- `ExecuteNodeAction` itself returns `(GraphNodeResult, error)` under the same
  rule, and `GraphNodeResult` must be able to say "refused: undeclared outcome"
  and "refused: unknown activity" as *outcomes of the call*, not only as errors.
  Unknown activity name is the `RunnerSet` rule verbatim: a reportable failure,
  never a nil dispatch.

This also constrains S1: `ActionSpec.Outcomes` for any realistic action must
include at least one failure outcome, or a failing handler has no legal outcome to
return and every failure degenerates into an error. Say so in `doc.go`.

---

## Deviations

### DEV-1 — TEST-2, recorded, not blocking

```
⚠️ STANDARD DEVIATION (recorded, proceeding)
Rule: TEST-2 — "go.mod has exactly four direct requires (go-cmp, go-github,
      yaml.v3, modernc.org/sqlite) and SHOULD keep that shape"
Proposed: a fifth direct require, gonum.org/v1/gonum v0.16.0, for DOT decoding
Reasoning: issue #315 specifies Gonum-based DOT parsing; TEST-2's prohibition
      clause is MUST NOT for mocking frameworks and SHOULD for the four-require
      shape, so this is a reasoned deviation, not a rule breach. Risk is
      contained by Decision 1: one file, one import, pinned by a purity test in
      config/purity_test.go's shape.
Obligation: APP_CONSTRAINTS.md's preamble requires this be recorded in
      .aiteam/AC_AMENDMENTS.md. That record is a task in S1 (T-001.10), not a
      side effect of someone remembering.
Decision needed from: nobody — taken.
```

### DEV-2 — owner decision carried alongside: what if the module proxy is unreachable

EO-2 is the only thing in this plan that can block every story at task one, and
it cannot be resolved from inside Plan. **This does not stop story authoring or
freeze; it is a contingency the owner can answer in one comment, and until then
the plan proceeds as written (S1 T-001.1 first, everything gated on it).**

- **The conflict.** `gonum.org/v1/gonum` is absent from `go.mod`, `go.sum` *and*
  the local module cache. If the implementation sandbox has no module-proxy
  access, `go get` fails, `go build ./...` fails, and S1–S5 are blocked in order.
- **The impact.** S1 blocks at its first task; S3, S4 and S5 block transitively
  (S2 alone survives — see the ordering note below, which is why S2 is
  deliberately made independent). No partial delivery is possible from S1 without
  a decision.
- **The options.**
  - (a) **Block and report.** Zero design change; the run reports a truthful
    blocked story and the dependency is added in a network-capable environment
    later. Cost: the whole feature waits on infrastructure.
  - (b) **Hand-rolled DOT subset parser**, stdlib-only, covering the attribute
    grammar these profiles actually use. Cost: roughly one extra day in S1 and a
    parser this team now owns and must fuzz — but it *satisfies* TEST-2 instead of
    deviating from it, and thanks to Decision 1 it replaces exactly one file.
  - (c) **Vendor** `gonum.org/v1/gonum` into the repo. Cost: platform guidance
    says vendoring is not the default and this repo does not vendor; it also needs
    network access once anyway, so it only helps if a cache can be primed
    out-of-band.

Recommended if the owner does not answer: proceed with (a) and let the blocked
story report the fact. Do not silently substitute (b) — it is a scope change.

---

## Part 2 — Per-task hints

Owner role per the spec's own boundary note; `go-implementation-engineer` builds,
`go-unit-test-engineer` and `go-integration-test-engineer` prove. Task
decomposition is per-story content for the sub-issues.

### Story S1 — `internal/graphexec` core + Gonum dependency

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-001.1 | Add `gonum.org/v1/gonum v0.16.0` to go.mod/go.sum | infra | — | XS |
| T-001.2 | `graphexec` types + `doc.go` (Profile/Node/Edge/ActionSpec/Registry) | data | — | S |
| T-001.3 | `dot.go`: Gonum decode → `Profile`, attribute preservation | logic | T-001.1, T-001.2 | L |
| T-001.4 | `Validate` with aggregated faults | logic | T-001.2 | M |
| T-001.5 | `Decide` | logic | T-001.2 | XS |
| T-001.6 | Reserved attributes + `InvokeAgentConfig` | logic | T-001.2 | S |
| T-001.7 | Unit tests: parse round-trip incl. EO-1 fixtures | test | T-001.3 | M |
| T-001.8 | Unit tests: validation fault table, bounded cycle, `Decide` totality | test | T-001.4, T-001.5 | M |
| T-001.9 | Purity/containment test | test | T-001.3 | S |
| T-001.10 | `AC_AMENDMENTS.md` entry for DEV-1 + APP_CONTEXT package-map edge | infra | T-001.1 | XS |

#### T-001.1: Add the Gonum dependency
**Agent:** go-implementation-engineer

- **Pattern:** Dependency add, nothing else. `go get gonum.org/v1/gonum@v0.16.0`
  then `go mod tidy`, then `go build ./...`.
- **Package placement:** `go.mod`, `go.sum`. No Go file changes in this task.
- **Integration points:** None yet — this task exists separately precisely so its
  failure is diagnosable on its own (EO-2). If it fails, the story is blocked
  here and the report says so truthfully; do not work around it by writing a
  parser (that is DEV-2's owner decision, not yours).
- **Watch out for:** `go mod tidy` will also move indirect requires around; the
  criterion is "no *other* dependency added or upgraded" — check the diff of the
  indirect block, do not assume. Go uses MVS, so `@v0.16.0` is a floor, not a
  pin; verify `go.sum` records v0.16.0 and not something higher pulled in by the
  module graph.
- **Not in scope:** Any import of gonum. That is T-001.3.

#### T-001.2: `graphexec` types and package specification
**Agent:** go-implementation-engineer

- **Pattern:** New pure package, same class as `internal/planbrief` and
  `internal/naming`. STYLE-7 and APP_CONTEXT's convention: the package
  specification goes in `internal/graphexec/doc.go`, and it is the first file
  written, not the last.
- **Package placement:** `internal/graphexec/graphexec.go` (types),
  `registry.go` (Registry), `doc.go`.
- **Key interfaces:** None. `Registry` is a **concrete struct** wrapping
  `map[string]ActionSpec`, not an interface — there is one implementation and no
  substitution need, and TEST-5 forbids declaring an interface next to its
  implementation. `Validate(p *Profile, reg Registry) error` takes the concrete
  type.
- **Concurrency/context:** No `ctx` anywhere in this package — nothing here can
  block. `Registry` is build-then-freeze and shared across turns exactly like
  `turn.RunnerSet`; say that in its doc comment and clone slices on `Register`
  and `Resolve` per Decision 3.
- **Integration points:** `agent.Mode` (`internal/agent/mode.go:25-30`) for
  `Profile.Mode` and `ActionSpec.Mode`; `config.Tier` for the tier attribute.
  Nothing else internal.
- **Watch out for:** `ProfileVersion`, never `WorkflowVersion` — that identifier
  means managed content/submission schema generation in 30+ files and a collision
  would be a permanent reading hazard. In `doc.go`, state the three things a
  future reader will otherwise get wrong: (1) cycles are legal, (2) `attempt`
  counts every dispatch (Decision 5), (3) `lf_model_tier`/`lf_agent_markdown` are
  the only action-specific attributes this package knows, and a third one moves
  them to a declarative schema on `ActionSpec` (Decision 7).
- **Not in scope:** Parsing, validating, deciding.

#### T-001.3: DOT decode into `Profile`
**Agent:** go-implementation-engineer

- **Pattern:** Adapter file. This is the *only* file in the tree that imports
  `gonum.org/v1/gonum`, and the conversion from Gonum's representation to
  `graphexec`'s own types happens entirely inside it. Model it on how
  `internal/config`'s `config.go` is the single yaml.v3 importer.
- **Package placement:** `internal/graphexec/dot.go`. Exported entry point
  `ParseProfile(dot []byte) (*Profile, error)`.
- **Key interfaces:** Whether you reach for the `graph/formats/dot` AST or a
  carrier type implementing Gonum's attribute setters is your call — the test in
  T-001.7 is the proof, not the technique.
- **Integration points:** `ParseProfile` takes bytes. It does not open a file, does
  not know a root, and must not grow a `ParseProfileFile` sibling (OQ-2 is
  deliberately unanswered).
- **Watch out for:** (1) `Digest` is a hex sha256 of the bytes *as supplied*,
  computed with `internal/team/hash.go`'s `HashBytes` semantics — do not
  normalise, trim or re-serialise before hashing, or the pin will not match the
  file on disk. Reimplement the two-line hash locally rather than importing
  `internal/team`; `graphexec → team` is not an edge anyone wants. (2) Every raw
  `lf_*` attribute lands in `Node.Attrs` unfiltered — an unknown attribute is data
  for a future feature, not an error. (3) Gonum's decoder is the whole reason EO-1
  exists: attributes arriving via a standalone graph statement or a `node [...]` /
  `edge [...]` default statement are the ones most likely to vanish silently.
  Silent loss must be impossible to ship, which is why T-001.7 asserts survival
  rather than trusting the decoder.
- **Not in scope:** Semantic validation (T-001.4). A structurally decodable
  profile with an unknown action is `ParseProfile`'s success and `Validate`'s
  fault.

#### T-001.4: `Validate` with aggregated faults
**Agent:** go-implementation-engineer

- **Pattern:** Aggregate every independent fault with `errors.Join` — STYLE-9 and
  CFG-5's rule, the same shape `team.Decode` and `Config.Validate` use. First-error-
  wins is a defect here: an author fixing a profile should see all eleven problems
  at once.
- **Package placement:** `internal/graphexec/validate.go`.
- **Key interfaces:** Consumes the concrete `Registry`. Validity is registry
  membership, never a switch.
- **Watch out for:** (1) **Do not reuse or imitate
  `internal/planbrief/typed_decode.go:133-156`** — it enforces acyclicity, which
  is the exact opposite of this requirement. Cycles are legal; what is illegal is
  a node on a cycle with no `lf_limit > 0`. You need cycle *membership* detection
  (SCC or a back-edge walk), not a DAG check. (2) TEST-7: fault order must be
  deterministic — iterate nodes and edges in declared order, or sort; never in Go
  map order. (3) Reachability is two separate checks in two directions: every node
  reachable from `Start`, and every path reaching some terminal. Both must be
  faults, and "no terminal at all" must not be reported as eleven unreachability
  faults.
- **Not in scope:** Anything I/O. No `os`, no `time`.

#### T-001.5: `Decide`
**Agent:** go-implementation-engineer

- **Pattern:** Total function. `Decide(p, node, outcome) (next string, ok bool)`
  returns `ok=false` for anything undeclared and never guesses. `ok bool` rather
  than an error is right here — this is a lookup, not an attempted operation.
- **Package placement:** `internal/graphexec/decide.go`.
- **Watch out for:** `Decide` must not re-run validation. It assumes a validated
  profile, so it can be a map lookup; say so in its doc comment, since a caller
  passing an unvalidated profile is the one way to get nonsense out of it.

#### T-001.6: Reserved attributes and `InvokeAgentConfig`
**Agent:** go-implementation-engineer

- **Pattern:** Typed view over `Attrs`, per Decision 7.
- **Package placement:** `internal/graphexec/invoke_agent.go`. **Not
  `internal/turn`** — the spec's API table is wrong on this row and story
  authoring corrects it.
- **Key interfaces:** `InvokeAgentConfig{AgentMarkdownRef string; ModelTier
  config.Tier}`. `ModelTier` is typed `config.Tier`, validated with `Tier.Valid()`
  (`internal/config/tier.go:26-38`). Resolution to an actual model stays with
  `Tiers.ResolveModel` at execution time (`internal/turn/delivery.go:69-72`) and
  is not touched here.
- **Watch out for:** (1) Mirror `UnknownTierError`'s rule exactly — an
  unrecognised tier is a fault naming the key, the value and the three valid
  tiers; never a silent default to `balanced`. (2) `AgentMarkdownRef` gets the
  syntactic path check from Decision 6: non-empty, relative, slash-separated, no
  `.`/`..`, no backslash, no leading `/`. Do **not** resolve, `Stat` or open it —
  that is #316's job and it needs the rooted/symlink discipline that lives in
  `teamsource`, not here.
- **Not in scope:** Any handler. This is a type and a decoder (A-10).

#### T-001.7: Parse round-trip tests, including EO-1
**Agent:** go-unit-test-engineer

- **Pattern:** Table-driven (TEST-1), stdlib `testing`, `go-cmp` sparingly.
  Package `graphexec_test` — black-box, since everything under test is exported
  and this package has no in-package test convention to inherit (TEST-4).
- **Package placement:** `internal/graphexec/parse_test.go`.
- **Watch out for:** This test *is* evidence obligation EO-1 — the claim "Gonum
  drops `lf_*` attributes" is unverified and the test, not the claim, settles it.
  The fixture must carry, at minimum: a standalone graph-attribute statement, a
  `node [...]` default statement, an `edge [...]` default statement, and
  per-node/per-edge inline attributes, and must assert each value survives into
  `Profile`/`Attrs`. DOT strings are inline Go string literals — no `.dot` file is
  added to the repository (an S1 criterion, and shipping one is #316's).
- **Not in scope:** Validation cases (T-001.8).

#### T-001.8: Validation and decision tests
**Agent:** go-unit-test-engineer

- **Pattern:** One table covering all eleven named fault classes, asserting the
  *set* of faults for a profile carrying several at once — that is what proves
  aggregation rather than first-error.
- **Package placement:** `internal/graphexec/validate_test.go`, `decide_test.go`.
- **Watch out for:** The positive case is as load-bearing as the negatives: a
  profile with a bounded cycle must validate **successfully**. A future reader
  will assume cycles are bugs unless a passing test says otherwise.

#### T-001.9: Purity and containment test
**Agent:** go-unit-test-engineer

- **Pattern:** Copy `internal/config/purity_test.go`'s structure directly —
  `go/parser` with `parser.ImportsOnly` over every non-test `.go` file in the
  package directory, one allowed file and one allowed third-party import.
- **Package placement:** `internal/graphexec/purity_test.go`.
- **Watch out for:** Assert two things, not one: (1) only `dot.go` imports
  `gonum.org/v1/gonum` and no other third-party package appears anywhere; (2) no
  file imports `internal/store`, `internal/daemon`, `internal/turn`,
  `internal/harness`, `internal/codex`, `os`, `os/exec`, `net/http` or `time`.
  TEST-10 applies the moment this test fails: it is a design change, never a test
  to relax.

#### T-001.10: Record DEV-1 and update the package map
**Agent:** go-implementation-engineer

- **Pattern:** Documentation obligation with a named home.
  `.aiteam/AC_AMENDMENTS.md` gets the TEST-2 deviation entry (rule id, reason,
  containment); `.aiteam/APP_CONTEXT.md`'s package map gets `graphexec → agent,
  config` and `turn → … graphexec`, and the "Do not reinvent these" table gets a
  row for graph profile parsing.
- **Watch out for:** APP_CONSTRAINTS' preamble makes the amendment record
  mandatory, not optional. Add nothing about the four other decisions here —
  they are choices within the rules, not deviations from them.

### Story S2 — `internal/store` graph persistence

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-002.1 | Migration `0048_graph_execution_foundations.sql` | data | — | M |
| T-002.2 | `GraphDefinition` accessors | data | T-002.1 | S |
| T-002.3 | `GraphInstance` accessors + `ListDueGraphInstances` | logic | T-002.1 | M |
| T-002.4 | Attempt accessors + sentinels | logic | T-002.1 | M |
| T-002.5 | `CompleteGraphNodeAttempt` composite transaction | logic | T-002.3, T-002.4 | M |
| T-002.6 | Schema and migration tests | test | T-002.1 | S |
| T-002.7 | Accessor invariant tests | test | T-002.5 | L |

**S2 is deliberately independent of S1** — it imports no `graphexec` type and
needs no new dependency, so it survives a DEV-2 block. Sequence it in parallel
with S1, not behind it.

#### T-002.1: Migration 0048
**Agent:** go-implementation-engineer

- **Pattern:** One file, five tables, five indexes — the `0019_*.sql` shape
  (six tables, four indexes, one file). New files self-register via
  `//go:embed migrations/*.sql` (`internal/store/migrate.go:20`) and order by
  numeric prefix; there is no registration code to touch.
- **Package placement:** `internal/store/migrations/0048_graph_execution_foundations.sql`.
- **Integration points:** FK targets `sessions(id)` and `runs(id)` exist
  (`0001_init.sql:50,84`). `foreign_keys=ON` is a DSN pragma, so these are
  enforced — declare parents before children.
- **Watch out for:** (1) Indexes per Decision 8; SQLite indexes neither a child FK
  column nor a `next_due_at` filter for you. (2) `team_snapshot_digest` nullable
  (Decision 8). (3) No column or table named `workflow_*`. (4) Timestamps are TEXT
  holding `store.FormatTime` output (UTC RFC 3339, ms) so lexical order matches
  chronological order — the poller's `next_due_at <= now` comparison depends on
  that property. (5) Do not edit any existing migration. (6) If another branch
  lands a 0048 before merge, renumber; the number is a sequence position, not
  identity.
- **Not in scope:** Go accessors.

#### T-002.2: `GraphDefinition` accessors
**Agent:** go-implementation-engineer

- **Pattern:** One file per entity (`internal/store/graph_definitions.go`), `ctx`
  first (CONC-5), errors wrapped `store: <verb>: %w`.
- **Key interfaces:** `CreateGraphDefinition` is get-or-create on
  `(team, mode, digest)` under one `BEGIN IMMEDIATE` — the read-then-write rule
  in `store/store.go:38` and the shape `CreateOrGetConflictProposal` already uses
  (`store/conflict_proposals.go:281`).
- **Watch out for:** Same path + different digest is a new row, not an update or a
  conflict — a definition row is immutable evidence of what was pinned. Apply
  Decision 6's syntactic path check before the insert and return a typed refusal.
- **Not in scope:** Reading the file the path names. Ever, in this package.

#### T-002.3: `GraphInstance` accessors
**Agent:** go-implementation-engineer

- **Pattern:** `internal/store/graph_instances.go`. `ListDueGraphInstances` mirrors
  `ListBacklogSessions`' shape (`store/sessions.go:389`): a shared column-list
  const, a `scanGraphInstance` helper, `defer rows.Close()`, `rows.Err()` checked.
- **Watch out for:** (1) The due predicate is `status='running' AND next_due_at IS
  NOT NULL AND next_due_at <= ?` — a NULL `next_due_at` is *never* due, and
  `cancelled`/terminal instances are never returned. Write the NULL case as its
  own test, because SQL's NULL comparison silently gives the right answer here for
  the wrong reason and a later refactor can break it invisibly. (2) `ORDER BY
  next_due_at, id` and honour the caller's limit — deterministic order is TEST-7,
  not a nicety. (3) `now` is a parameter; this package never reads a clock for a
  domain timestamp (`store/store.go:58-60`).

#### T-002.4: Attempt accessors and sentinels
**Agent:** go-implementation-engineer

- **Pattern:** `internal/store/graph_node_attempts.go`. Typed sentinels in this
  package's established shape — `var ErrGraphAttemptActive = errors.New("store:
  a graph node attempt is already running")`, `ErrGraphAttemptTerminal`, matching
  `ErrStoryProgressBusy` / `ErrCreationConflict` — checked with `errors.Is`, never
  by string (STYLE-5).
- **Key interfaces:** `StartGraphNodeAttempt` allocates the next `attempt` for
  `(instance, node)` and inserts `running`, all inside one `BEGIN IMMEDIATE`.
- **Watch out for:** (1) `attempt` counts every dispatch including loop re-entries
  (Decision 5) — allocate it as `MAX(attempt)+1` for the `(instance, node)` pair
  inside the transaction, never from a caller-supplied value. (2) A refusal here is
  an error, not a result value: these are invariant violations the caller must
  branch on, which is CONC-3's "could not be conducted" arm. (3) Finishing an
  already-terminal attempt is `ErrGraphAttemptTerminal`, never a silent UPDATE —
  attempt rows are evidence. (4) A non-NULL `outcome` on a non-`succeeded` status
  is refused in Go *and* worth a CHECK in the schema; belt and braces are cheap on
  immutable evidence.
- **Not in scope:** Deciding *when* an attempt is stale. A crash mid-dispatch
  leaves a `running` attempt that blocks the next one; recovery is
  `FinishGraphNodeAttempt(…, failed)` and the staleness *policy* belongs to
  whoever wires the daemon (#316). Say that in the accessor's doc comment so the
  wedge is documented rather than discovered.

#### T-002.5: `CompleteGraphNodeAttempt`
**Agent:** go-implementation-engineer

- **Pattern:** The composite transaction from Decision 2 — the single accessor
  `ExecuteNodeAction` calls after dispatch. Use `db.withTx`
  (`store/store.go:176`).
- **Package placement:** `internal/store/graph_node_attempts.go` alongside T-002.4.
- **Key interfaces:** In one transaction: finish the attempt (status, outcome,
  finished_at), insert the `graph_outputs` row when there is one, insert the
  `graph_transitions` row when the outcome transitions, and update
  `graph_instances.current_node` / `status` / `next_due_at`.
- **Watch out for:** (1) A refused outcome writes the attempt as `failed` and
  **no** transition row and leaves `current_node` unchanged — that asymmetry is an
  S3 criterion and it has to be expressible in one call. (2) `graph_transitions`
  is append-only: this accessor only ever INSERTs into it, and no accessor in this
  feature UPDATEs or DELETEs it. (3) The caller passes the already-decided target
  node; this accessor does not import `graphexec` and does not decide anything.
- **Not in scope:** Dispatch, retries, backoff.

#### T-002.6: Schema and migration tests
**Agent:** go-unit-test-engineer

- **Pattern:** In-package (`package store`) — the convention throughout
  `internal/store` (`store_test.go`, `meter_test.go`). Real SQLite via
  `store.Open` + `Migrate`; TEST-6 forbids proving a transaction against a fake.
- **Watch out for:** Assert `SchemaVersion` reaches 48, that a second `Migrate`
  is a no-op, and — reading `sqlite_master` — that no table, column or index in
  this feature is named `workflow_*`. Also assert the five indexes exist; an index
  that silently fails to be created is invisible until the poller is slow in
  production.

#### T-002.7: Accessor invariant tests
**Agent:** go-unit-test-engineer

- **Pattern:** Table-driven per accessor, real migrated SQLite.
- **Watch out for:** The invariants worth a named test each are the refusals, not
  the happy paths: duplicate `(instance, node, attempt)`, a second attempt while
  one is `running`, finishing a terminal attempt, a non-NULL outcome on a failed
  attempt, a NULL `next_due_at` never selected, a `cancelled` instance never
  selected, and the append-only trace (the earlier transition row byte-identical
  after a second pass over the same node). Close and reopen the DB to prove
  durability — that is what S5 later builds on.

### Story S3 — `internal/turn` activity layer

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-003.1 | Activity types + registry | data | S1 T-001.2 | S |
| T-003.2 | `ExecuteNodeAction` | logic | T-003.1, S2 T-002.5 | L |
| T-003.3 | Fake handler fixture + dispatch tests | test | T-003.2 | M |
| T-003.4 | Inertness structural test | test | T-003.2 | S |

#### T-003.1: Activity types and registry
**Agent:** go-implementation-engineer

- **Pattern:** Registry-driven dispatch, modelled on `turn.RunnerSet`
  (`internal/turn/execute.go:33-43`) — read its doc comment first; it explains
  exactly why validity is registry membership and never a switch, and the same
  sentences apply verbatim here.
- **Package placement:** `internal/turn/activity.go`.
- **Key interfaces:** `ActivityHandler` is declared **here**, in the consuming
  package (TEST-5), and has one method. It is justified despite having no
  production implementation yet because a test fake requires substitution *and*
  because a later issue supplies the real one — that is the seam this story
  exists to prove.
- **Concurrency/context:** `ctx` first on `Execute`. `ActivityRegistry` is built
  once at startup and shared, like `RunnerSet` — immutable after construction, no
  mutex, documented as such. `ActivityRequest.Attrs` is cloned on construction
  (CONC-1, Decision 3); a handler must not be able to mutate a shared `Profile`.
- **Watch out for:** `ActivityResult` needs a `Diagnostic string` field — Decision
  9. Without it, this story's own criterion about preserving a handler's
  diagnostic cannot pass, and CONC-3's split between a failed activity (a result)
  and an activity that could not be conducted (an error) has nowhere to live.
  Document that split on the `ActivityHandler` interface, since it is the contract
  every future handler author reads.
- **Not in scope:** Any real handler. `InvokeAgentConfig` is in `graphexec`
  (Decision 7) and is not redeclared here.

#### T-003.2: `ExecuteNodeAction`
**Agent:** go-implementation-engineer

- **Pattern:** Straight-line orchestration across three packages; the whole point
  is that it contains no policy of its own. `internal/turn/graph_node.go`.
- **Key interfaces:** Consumes `graphexec.Registry`/`Decide` and the store
  accessors; returns `(GraphNodeResult, error)` under Decision 9's rule.
- **Concurrency/context:** The ordering is the design, and it is binding:
  `StartGraphNodeAttempt` (tx) → resolve handler → **dispatch with no transaction
  open** (CONC-18) → `Decide` → `CompleteGraphNodeAttempt` (tx). No goroutine
  (CONC-2's budget is unchanged). No `time.Sleep`, no `time.After`, no retry loop
  — retry and backoff are `internal/daemon`'s, and CONC-6's "no sleeps below the
  daemon" is the rule that makes it so.
- **Integration points:** `turn.Options` carries `DB`; follow the package's
  existing convention of taking `Options` rather than inventing a second
  dependency bundle (STYLE-3). Handlers receive `Options` too — accept that for
  consistency, but note in the doc comment that a handler must not retain it.
- **Watch out for:** (1) Outcome membership is checked **here**, against the
  node's declared outcomes, not inside the handler — a handler is untrusted about
  what it returns. An undeclared outcome records `failed`, writes no transition
  and leaves `current_node` alone. (2) An unknown activity name is a reportable
  failure, never a nil dispatch — the `RunnerSet` rule, and an existing enforced
  invariant elsewhere in this file. (3) A terminal node dispatches nothing,
  creates no attempt row, and sets the instance `succeeded`. (4) When a handler
  errors, the recorded diagnostic must say what actually happened; #259's harvest
  lesson is that a post-execution fault reported as "the agent did not start" is
  worse than no diagnostic at all.
- **Not in scope:** Being called by anything. No production call site exists in
  this scope, and T-003.4 asserts it.

#### T-003.3: Fake handler and dispatch tests
**Agent:** go-unit-test-engineer

- **Pattern:** Hand-written fake struct (TEST-2 forbids a mocking framework), in
  package `turn` — `internal/turn` is tested in-package throughout and TEST-4
  forbids the `_internal_test.go` suffix here. Extend the existing turn fixtures:
  real migrated SQLite, fake runner (TEST-3).
- **Watch out for:** Every assertion is about rows that exist afterwards, not
  about calls that happened — "exactly one attempt row, one output row, one
  transition row, and `current_node` advanced" is observable state, and that is
  what makes this test outlive a refactor. Prove the refusal path by row *absence*
  (no transition row, `current_node` unchanged) as explicitly as the happy path.

#### T-003.4: Inertness structural test
**Agent:** go-unit-test-engineer

- **Pattern:** `go/parser` over the tree, in the shape of
  `daemon/clock_test.go` — that file is the worked example of an AST-level
  invariant with a comment explaining why it exists.
- **Watch out for:** Assert two things: no non-test file calls
  `ExecuteNodeAction`, and no graph file in `internal/turn` references
  `time.Sleep`/`time.After`. This is EO-4, and inertness is a deliverable — when
  #316 wires a real call site, this test is *supposed* to fail and be deliberately
  changed (TEST-10), which the comment should say out loud.

### Story S4 — `internal/daemon` poller skeleton

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-004.1 | `pollDueGraphInstances` | logic | S2 T-002.3 | S |
| T-004.2 | `nextGraphAttemptAt` + `retryPolicy` + cap constant | logic | — | S |
| T-004.3 | Poller and backoff tests | test | T-004.1, T-004.2 | M |

#### T-004.1: `pollDueGraphInstances`
**Agent:** go-implementation-engineer

- **Pattern:** Thin selection wrapper, unexported, unwired.
  `internal/daemon/graph_poll.go`.
- **Key interfaces:** `(ctx, *store.DB, now time.Time, limit int) ([]store.GraphInstance, error)`.
  `now` is a parameter — it must not read `d.now`, which would put a second
  clock reader below `Tick` and break the invariant `daemon/clock_test.go` guards.
- **Integration points:** `internal/store` only. **Does not import
  `internal/graphexec`** (Decision 4).
- **Watch out for:** It selects and returns; it does not dispatch, mark, or mutate.
  The "mark and select" shape of `ListBacklogSessions` is the read half only here.
- **Not in scope:** Any `Tick` call site.

#### T-004.2: `nextGraphAttemptAt` and the retry policy
**Agent:** go-implementation-engineer

- **Pattern:** Pure function over values; `internal/daemon/graph_backoff.go`.
  Borrow the *discipline* of `internal/daemon/backoff.go` — "hold an instant,
  never sleep, never cancel in-flight work" — but not the type: `backoff` there is
  unexported and specific to forge rate-limit refusals, and reusing it would
  couple graph retries to GitHub's failure vocabulary.
- **Key interfaces:** `nextGraphAttemptAt(attempt int, last time.Time, policy
  retryPolicy) (time.Time, bool)`. `retryPolicy` carries the node's `lf_limit` as a
  plain `int`, the consecutive-failure count, the growth factor and the cap — so
  no `graphexec` import is needed.
- **Watch out for:** Decision 5's consequence: the backoff input is the count of
  *consecutive failed* attempts, which is not `attempt` once bounded loops are in
  play — a successful loop re-entry resets it. Getting these confused produces a
  backoff that grows across a healthy refine loop, which is a subtle production
  bug and an easy test to write now. The cap is a named constant in this package
  (`MaxBackoff`'s neighbour), never a literal at the call site.
- **Not in scope:** Sleeping, blocking, holding a `context.CancelFunc`, or
  deciding the *node*.

#### T-004.3: Poller and backoff tests
**Agent:** go-unit-test-engineer

- **Pattern:** Both functions are unexported, so these are
  `graph_poll_internal_test.go` / `graph_backoff_internal_test.go` in
  `package daemon` — TEST-4's rule for `internal/daemon`. Behavioural tests that
  need a real store stay black-box in `package daemon_test`.
- **Watch out for:** TEST-8 is absolute here — no `time.Sleep`, no `Eventually`,
  no wall-clock polling. These functions take their instant as a parameter
  precisely so a test can pass a fixed one. Follow the existing
  `TestBackoffTouchesNoContext` pattern for the structural assertion, and add the
  `Tick`-does-not-reference-the-poller assertion alongside `clock_test.go`'s
  companion checks.

### Story S5 — composition against a real database

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-005.1 | Composition fixture (real DB + registry + fake handler) | test | S2, S3 | M |
| T-005.2 | Happy-path traversal test | test | T-005.1 | M |
| T-005.3 | Bounded-loop and reopen-and-resume tests | test | T-005.2 | M |
| T-005.4 | Full-build and `make check` gate | infra | all | S |

#### T-005.1: Composition fixture
**Agent:** go-integration-test-engineer

- **Pattern:** Extend the existing turn fixture rather than build a parallel one
  (TEST-3): real migrated SQLite via `store.Open` + `Migrate`, a hand-written fake
  `ActivityHandler`, a `graphexec.Registry` holding one `ActionSpec`, and a
  synthetic inline DOT string.
- **Package placement:** `internal/turn`, in-package (TEST-4).
- **Watch out for:** The fixture's fake handler must be *scriptable per node*
  (return outcome X the first time, Y the second) — that is what T-005.3's bounded
  loop needs, and retrofitting it later means rewriting T-005.2's assertions.

#### T-005.2: Happy-path traversal
**Agent:** go-integration-test-engineer

- **Pattern:** End-to-end over all four layers: parse → pin (path + computed
  sha256) → create instance → drive `ExecuteNodeAction` to a terminal node →
  assert the full `graph_transitions` trace equals the profile's expected path.
- **Watch out for:** Assert the trace as an ordered sequence, not a set — order is
  the whole claim, and `ListGraphTransitions`' determinism is what S2's index
  exists to provide. Do not assert on `id` values; assert on
  `(from_node, on_outcome, to_node)` triples.

#### T-005.3: Bounded loop and resume after reopen
**Agent:** go-integration-test-engineer

- **Pattern:** Two scenarios over the same fixture.
- **Watch out for:** (1) The bounded-loop case is where Decision 5 becomes
  observable: the loop reaches `lf_limit` counted as *dispatches*, stops with a
  truthful terminal status, and the full history stays visible in
  `graph_transitions`. If this test is hard to write, the `attempt` semantics are
  wrong, not the test. (2) The reopen case is what Decision 2's single completion
  transaction buys: close the DB mid-run, reopen, resume from the persisted
  `current_node`, and assert no duplicate attempt row. If the transaction were
  split, this test would pass or fail depending on where the close landed —
  which is exactly the bug it exists to exclude.

#### T-005.4: Full-build and check gate
**Agent:** go-implementation-engineer

- **Pattern:** `go build ./...`, `go test ./...`, `make check` (fmt, vet, pinned
  golangci-lint v2, race tests).
- **Watch out for:** A new package is new lint surface. `gosec`, `errorlint`,
  `nilerr`, `noctx`, `unparam` are on and `govet` is enable-all minus
  fieldalignment and shadow (STYLE-2) — expect `unparam` to notice an unused
  parameter on a skeleton function and resolve it by justifying the signature, not
  by deleting the parameter a later issue needs. `context.Background()` outside
  `main` and `fmt.Print*`/`log.*` are lint failures, not style notes.
- **Not in scope:** Committing, pushing, or any `.claude/**` / `.codex/**` edit.

---

## Security findings (forward-looking: risks this plan carries)

**Scope:** the five stories above. **Rulebook:** `.aiteam/APP_CONSTRAINTS.md` §1.

| # | Area | Status | Finding |
|---|---|---|---|
| 1 | Input validation | ⚠️ | DOT attributes are a new untrusted-ish input channel. Mitigated by Decision 6 (syntactic path refusal at entry) and by the business rule that no attribute value is ever evaluated, templated or executed — keep `Attrs` `map[string]string` and never `any`. |
| 2 | Path selection | ⚠️ | `lf_agent_markdown` and `graph_definitions.path` select files a later issue opens. SEC-20/SEC-21 apply at resolution time (rooted, `Lstat`, digest re-verify) — out of scope here, which is why Decision 6 constrains what can be stored. |
| 3 | Auth & authz | ✅ | No new authority: no forge call, no identity comparison, no permission decision. SEC-8's numeric-identity rule is untouched. |
| 4 | Secrets | ✅ | No secret is read, stored or passed. `graph_*` holds no credential and no config value. SEC-4's child-environment allowlist is untouched because no child is spawned. |
| 5 | Data handling | ⚠️ | `graph_outputs.payload` will hold agent-authored bytes. Nothing renders them in this scope; the moment something does, SEC-6's `redact.Redactor.Apply` applies at the posting boundary and the stored copy stays unredacted like `$LF_HOME/logs`. Document the column accordingly. |
| 6 | SQL injection | ✅ | All new SQL is static text with placeholder binding; no identifier or predicate is built from input. Keep `ListDueGraphInstances`' limit a bound parameter, not a `Sprintf`. |
| 7 | Supply chain | ⚠️ | One new direct dependency (DEV-1). Pinned in `go.sum`, contained to one file, checked by a purity test. `gonum.org/v1/gonum` is a large, actively maintained, BSD-3 numerical library — the DOT decoder is a small corner of it, and `go mod tidy` should not pull its numerical packages into the build graph. Verify that in T-001.1. |
| 8 | Error handling | ✅ | Typed sentinels with `store:` prefixes, `%w` wrapping, no stack traces leaving the process. SEC-5's renderer revalidation is unreached because nothing renders. |

**Summary:** the posture is good precisely because the scope is inert — no
subprocess, no credential, no forge call, no file read. The one durable risk this
plan can still get wrong is path selection from a DOT attribute, which is why
Decision 6 is binding now rather than at #316, when the value will already be in
the database.

## Platform constraint notes

- **Project layout** — new code lands in `internal/`; no `pkg/`, no new `cmd/`.
  Compliant.
- **Error handling** — new branchable conditions (`ErrGraphAttemptActive`,
  `ErrGraphAttemptTerminal`) get typed sentinels checked with `errors.Is`, per
  this repo's existing `store` sentinels. No `panic`; SEC-11 permits exactly one
  `recover()` in the tree and it is not here.
- **Concurrency** — no goroutine is added; CONC-2's documented budget is
  unchanged, and that is an assertion worth keeping true rather than a
  coincidence.
- **Dependency management** — one new direct require; `go.sum` committed
  alongside; `go mod tidy` after. See DEV-1 and DEV-2.
- **HTTP / API layer** — not applicable (none).
- **Database access** — migration file (not manual DDL), `ctx` on every query,
  placeholders only, `defer rows.Close()`. The single-connection pool cap in
  `store/store.go:29-42` is a correctness decision, not a default to revisit.
- **Testing** — table-driven, stdlib `testing`, `go-cmp` sparingly, hand-written
  fakes, `-race` via `make check`.
- **Logging & observability** — not applicable: no new log statement is required
  in this scope, and adding one would need the injected `*slog.Logger`, never
  `fmt.Print*`/`log.*` (STYLE-2).
- **Linting** — new package = new lint surface; T-005.4 accounts for it.

## Gaps noted, not filled

- **Spec self-contradiction on `InvokeAgentConfig`'s package** (API table says
  `turn`, S1's criteria say `graphexec`). Resolved by Decision 7; story authoring
  must correct one of the two rows or two stories build the same type.
- **`ActivityResult` has no field for the diagnostic S3's criterion requires.**
  Resolved by Decision 9.
- **No indexes are specified for five new tables.** Filled by Decision 8.
- **Stale `running` attempt after a crash** wedges `(instance, node)` forever.
  Recoverable today via `FinishGraphNodeAttempt(…, failed)`; the *policy* for
  detecting staleness is out of scope and belongs with whoever wires the daemon.
  Noted, not invented — but it should be written into the accessor's doc comment
  so #316 inherits a known gap rather than a surprise.
- **`docs/` is deleted on this branch** and `.aiteam/APP_CONTEXT.md:64` still
  links `../docs/decisions/nested-agent-evidence.md`. EO-5 is unsatisfiable as
  written. Immaterial here — every criterion in this plan is provable by a Go
  test, none asserts coding-agent runtime behaviour — but it becomes live the
  moment a later increment adds such a criterion. Outside this feature's scope to
  repair.
- **Compatibility admission (#315 scope 5) and token/time baselines (scope 6b)**
  remain #315 debt for a later increment, per A-9. Correctly carried, not dropped.
- **OQ-1 and OQ-2 stay open and are genuinely off this scope's critical path.**
  Confirmed: no story here resolves `<team>/<mode>.dot` against any root.
  `ParseProfile` takes `[]byte`, `graph_definitions.path` is stored and never
  opened, and `AgentMarkdownRef` is decoded and never resolved. The RE's split
  holds architecturally. Decision 6 is the only constraint #315 places on those
  answers, and it narrows none of the three options in either question.
