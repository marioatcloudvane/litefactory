# External agent evidence: capture and provenance

Apply the [nested-agent evidence decision](decisions/nested-agent-evidence.md)
before choosing a proof method. This procedure describes an operator prerequisite
outside managed runs; implementation and CI do not launch a live coding agent to
acquire fixtures. It introduces no automatic acquisition, service, permission
option, or change to the existing sandbox, authentication or child environment.

## Acquisition and transfer

1. Inventory each affected acceptance criterion: proof method, available source,
   version/context bounds, and missing prerequisite. Establish which historical
   event relationships the capture needs to retain and which promises require
   separate evidence. If a source is unavailable, keep the obligation unsatisfied.
2. A separately authorized operator captures outside litefactory managed runs in
   a scratch directory outside the repository, using a bounded, read-only probe
   and the existing security boundary. Record the actual invocation settings,
   environment context and binary version when known. Do not copy credentials,
   expand permissions, provoke quota exhaustion, or inspect/edit daemon runtime
   state as an acquisition shortcut. Unknown historical facts stay unknown.
3. Keep raw material in operator-controlled storage outside the repository while
   preparing a sanitized candidate. Preserve stream order and distinguish stdout,
   stderr, provider-owned events and generated text. Record selection boundaries,
   omitted events, exit/status observations and any clock assumptions. Never
   promote generated claims about an action into provider evidence of that action.
4. Remove tokens, authorization headers, cookies, private prompts, generated work,
   personal data, private paths and sensitive identifiers unless a safe substitute
   preserves an essential assertion. Use consistent, distinct substitutions for
   each parent, child, thread, session and other referenced identity; rewrite every
   reference with the same mapping. Keep the sensitive mapping outside the repo.
   Preserve equality, inequality, parentage, ordering and model/event associations
   needed by the criterion. If redaction destroys a required relationship, mark
   that evidence insufficient rather than filling the gap with invented events.
5. Review the sanitized candidate and provenance together before transfer. Parse
   each selected JSONL record; inspect both structured fields and free text for
   sensitive data. Compare retained relationships with the raw source through the
   authorized operator, and record who reviewed them and any unavailable checks.
   Compute SHA256 of the final sanitized bytes and record byte/record counts.
   A digest detects later changes; it does not establish authenticity by itself.
6. Transfer only the reviewed sanitized stream and provenance into the owning
   package's `testdata/` directory. Replay with offline fake executable/parser
   seams. Record exact assertions, test commands/results and their limits in the
   provenance. Repository review checks the checksum, source attribution,
   sanitization and assertions before the factory handles committing. Keep raw
   captures, credentials and sensitive substitution maps out of the worktree.

Sanitizing exported fixtures does not authorize rewriting local `$LF_HOME/logs`
or other runtime state. Outbound reports still use the existing redaction path;
redaction is hygiene, not permission to expose secrets to an agent.

## Reusable provenance template

Copy this template beside a candidate fixture. Placeholder text is a field to
complete, not evidence; use **unknown**, **unavailable** or **not applicable** with
a reason where appropriate. Record assumptions separately from observed facts.

```markdown
# Provenance: <fixture filename>

- Evidence kind: authentic selected capture / synthetic example (choose one).
- Source and collector: original stream locator/selection bounds and collector
  identity or safe attribution; explain any unavailable source or unknown author.
- Capture date/time: observed timestamps, timezone/offset, clock bounds; explicit
  unknowns and separately labeled assumptions.
- Environment: host OS, shell, working-directory category, top-level/nested
  context and relevant sandbox/environment allowlist facts, without private paths
  or secret values. Distinguish observed settings from inferred context.
- Agent: binary name and verified version; for historical captures use unknown
  if not established. A separately inspected upstream version is not this version.
- Invocation: relevant flags, input shape, transport, configuration and lifecycle
  settings; sanitize values and record omissions. No authentication secrets.
- Selection: stdout/stderr/event channels, selected records, ordering, omissions,
  exit/status observations and whether the selection is complete for each claim.
- Sanitization: fields removed/replaced and transformations applied; explain how
  stable distinct substitutions preserve references without publishing the map.
- Asserted relationships: retained parent/child identities, model associations,
  ordering or other exact relationships, with event/field references.
- Integrity: sanitized filename, SHA256, byte/record counts, JSONL parse result;
  raw-source comparison performed by whom and what could not be checked.
- Review: reviewer, date, sensitive-data inspection and relationship comparison
  results; unresolved gaps, assumptions and approval limits.
- Offline verification: criterion identifiers, exact test commands/results and
  assertions exercised by this fixture. Do not mark unrun checks as passed.
- Claim limits: supported historical observations and version/context bounds;
  excluded current/runtime/lifecycle claims and additional evidence required.
- Re-verification: when version/context changes, what separately authorized
  external observation and review would be needed; no live CI acquisition.
```

## What a fixture proves

An authentic capture represents selected historical events in its documented
context. A synthetic example is constructed to test parser, classification or
relationship handling; label it synthetic even when it reproduces a reported
signature. Never fabricate a stream or provenance and call it authentic.

Assertions must stay within retained observations and relationships. A fixture
alone does not prove present-day delivery, sandbox compatibility, conversation
lifecycle, or end-to-end runtime behavior. Those promises need separately
authorized evidence and review appropriate to the actual criterion. Missing
evidence remains an unsatisfied obligation; fixture replay does not silently
weaken a frozen contract. Obtain factory-supplied revisions and use factory-mediated
replanning where applicable before changing promised scope.

## Existing quota example

[`quota-authentic.jsonl`](../internal/codex/testdata/quota-authentic.jsonl) and
[`QUOTA_PROVENANCE.md`](../internal/codex/testdata/QUOTA_PROVENANCE.md) illustrate
selected diagnostic fields with explicit metadata limitations. The provenance
attributes them to September 5, 2026, stream `100/0006.jsonl`, lines 85–86. The
historical Codex CLI binary version is **unknown**. Europe/Vienna is an **assumed**
machine-local timezone, not recovered historical metadata; the derived 20:26Z
instant depends on that assumption. Separately inspected upstream rust-v0.153.4
does not identify the captured binary.

The example supports bounded quota-diagnostic classification. It contains no
conversation handle, prompt, generated work or credential and supplies no new
proof of child-model attribution or delivery/lifecycle behavior. Additional
constructed quota test cases are synthetic. This procedure neither rewrites that
historical fixture nor retroactively invents missing provenance fields.

## Bounded mapping of supplied historical reports

The following are planning aids based only on the supplied
[#183 narrative](https://github.com/marioatcloudvane/litefactory/issues/183).
The referenced reports were not revalidated; their full frozen criteria and
current forge state were not supplied. These rows are not replacement criteria,
story revisions, or a claim that either feature recovered.

| Supplied obligation | What an appropriate authentic capture could prove | Additional evidence needed | Disposition if unavailable |
|---|---|---|---|
| [#73](https://github.com/marioatcloudvane/litefactory/issues/73): per-child model attribution | In the documented historical invocation, retained provider events associate distinct child identities with the reported model values and preserve their parent references. | Factory-supplied exact criteria; source/provenance with verified or explicitly unknown binary version; complete identity/model relationships and offline assertions. Any present-day attribution promise also needs separately authorized current observation and review. | Attribution remains unsatisfied when an appropriate source or essential relationship is missing. Synthetic identities test handling only; the quota example supplies no attribution evidence. |
| [#125](https://github.com/marioatcloudvane/litefactory/issues/125): runtime delivery | Selected historical producer/consumer events show a specific payload or bundle identifier at observed points when those relationships survive sanitization. | Exact frozen criteria and capture context; independently observed receipt/use where promised, rather than a generated claim of delivery. Current runtime delivery requires separately authorized observation covering that path. | Delivery remains unsatisfied where capture coverage or recipient evidence is missing; parser replay cannot establish receipt or use. |
| #125: bundle/conversation lifecycle and parity acceptance | Ordered historical events can document specific create/use/resume/close relationships only where those events and stable identities are actually captured. | Exact lifecycle/parity criteria, complete relevant transitions, interruption/resumption evidence and comparable contexts for each promised agent. Current lifecycle or end-to-end parity needs separately authorized runtime evidence and review. | Unobserved transitions and parity obligations remain unsatisfied; a partial stream cannot establish the whole lifecycle or another agent's behavior. |

Before recovery is claimed, review every actual criterion against available
evidence, document remaining prerequisites, and use factory-mediated replanning
where the frozen contract must change. This document does not read or mutate
those issues and does not assert their current status.

## Structured helper prerequisite (content/submission contract 2)

Current availability: authentic dual-harness helper evidence is **unavailable**.
Offline fake-runner, Unix-socket, SQLite and fake-forge tests establish synthetic
mechanisms only. They do not demonstrate either actual agent invoking the helper
inside its supported sandbox. US-012 retains this unsatisfied external prerequisite.

A separately authorized operator-controlled capture must show, for EACH actual
Claude and Codex: helper invocation, returned diagnostics, corrected contribution
within the same run, and the correlated authoritative durable decision observed
before agent exit. Record actual binary versions, host/sandbox, run/attempt,
content/submission and workflow versions, request/receipt correlation, exact check
and snapshot references, and final observation. Distinguish accepted input,
recorded engineering, acceptance and remote-effect state. Preserve failure,
disconnect, pending or missing observations as such; unknown versions stay unknown.
Use the existing boundary and sanitize capabilities and credentials out of evidence.

This is an operator handoff, not permission to launch a nested paid agent, add a
fallback harness, widen permissions or mutate runtime state during engineering.
Validate consumes supplied sanitized retained evidence without fresh checks. The
missing obligation remains unverified unless a reasoned explicit numeric-owner
exception records its exact scope while preserving the unavailable facts.

### External capture procedure

Perform this outside any managed litefactory engineering run (Research, Plan,
Implement, Validate, Spike), using litefactory's ordinary top-level operating
mode: the daemon polling a real registered repository and dispatching one actual
`claude` or `codex` process for one comment turn, exactly as `internal/daemon`
and `internal/harness`/`internal/codex` already do in production. Do not nest a
second coding-agent invocation inside an engineering session to acquire this —
that is the boundary [the nested-agent decision](decisions/nested-agent-evidence.md)
already sets.

1. Select one target story already on content/submission contract 2
   (`internal/agent.SubmissionContractVersion`) and note the persisted workflow
   version it exercises (`internal/turn/submissions.go`, `StartSubmissionSession`).
   Run one turn with the harness configured to `claude`, and separately one turn
   with the harness configured to `codex`. Each real turn allocates its own
   descriptor, socket and capability (`internal/submission/broker.go`); do not
   reuse or splice frames across attempts or harnesses.
2. During each real turn the factory appends the actual absolute helper
   executable path and the actual attempt descriptor path into the agent's own
   prompt (`internal/turn/submission_runner.go:runSubmissionBound`) and instructs
   it to call, on its own initiative, as ordinary shell tool use inside its own
   sandboxed session:
   ```sh
   "$helper" submission preflight --descriptor "$descriptor" < contribution.json
   "$helper" submission submit    --descriptor "$descriptor" < contribution.json
   "$helper" submission receipt   --descriptor "$descriptor" < receipt-query.json
   ```
   The operator does not construct these calls. Never hand-craft or replay a
   call outside the agent's own session and label the result authentic.
3. After the turn completes, locate that attempt's already-recorded artifacts
   without editing or replaying them:
   - The run log and metadata at
     `<LF_HOME>/logs/runs/<forge>/<owner>/<repo>/<issue>/NNNN.jsonl` and
     `NNNN.meta.json` (`internal/naming.RunLogPath`, `RunMetaPath`).
   - For Claude: the session transcript(s) under the isolated
     `CLAUDE_CONFIG_DIR` that run used (`projects/<escaped cwd>/<session>.jsonl`;
     see `internal/harness/usage_transcripts.go`), which record the shell
     tool-use entries invoking `submission preflight`/`submit`/`receipt` and
     their stdout.
   - For Codex: the associated rollout JSONL under that attempt's `HOME`
     (see `internal/codex/usage_rollout.go`), which records the equivalent
     shell invocations and their output.
   These sources already exist for usage accounting; this procedure reads them
   for a second purpose, it does not add a new collection path.
4. From those sources, extract only the request/response material AC-003 needs:
   the JSON sent on stdin for a `preflight` call that returned
   `ContributionDiagnostics` (`internal/agent/contribution.go`: `Path`, `Code`,
   `Expected`, `Actual`, `complete`), the corrected JSON sent on a following
   `preflight` or `submit` call for the same story/stage, the `submit` response's
   `request_id`, and the later `receipt` response distinguishing input validity,
   durable engineering record, acceptance decision and pending/failed remote
   effects (`internal/submission/protocol.go`: `Response`). Preserve relative
   order and which frames came from which harness's attempt.
5. Sanitize following the "Acquisition and transfer" steps above: keep raw
   material outside the repository, replace parent/child/session/thread/run
   identities with stable per-identity substitutions, strip tokens, cookies,
   private prompts, generated file contents and personal data, and keep the
   sensitive mapping outside the repository. Preserve the request/receipt
   correlation (matching `request_id` values), the stage/outcome fields and the
   diagnostic path/code/expected/actual fields exactly — those relationships are
   what the criterion needs.
6. Complete one provenance record per harness using the template above, review
   both sanitized candidates together with their provenance, compute SHA256 over
   each final sanitized file, and hand the reviewed pair to the operator's normal
   evidence transfer. Validate reads what was supplied; it does not run or
   commission a fresh capture.

### Provenance fields specific to this prerequisite

In addition to the general template above, each of the two sanitized captures
(one Claude, one Codex) must record:

- Actual tool/model version observed for that attempt (the Claude Code CLI
  version, or the Codex CLI version); unknown stays unknown, never assumed from
  an unrelated install.
- Host/sandbox identity: platform, whether the harness ran under the daemon's
  ordinary top-level sandbox for that attempt, and any sandbox/allowlist facts
  relevant to whether the helper's Unix socket was reachable.
- Run/attempt identifiers: the run ordinal (`NNNN` from `RunLogPath`/
  `RunMetaPath`) and which submission attempt (descriptor/socket generation) the
  frames came from, sanitized to a stable substitute where the raw value is
  sensitive.
- The exercised content/submission contract version
  (`agent.SubmissionContractVersion` at capture time) and the persisted workflow
  version bound for that story.
- Request/receipt correlation: the `request_id` values tying a `submit` call to
  its later `receipt` call, and the story/stage/outcome the contribution
  targeted.
- The final observation: whether a durable engineering or acceptance record was
  actually seen for that `request_id`, or whether the capture ended pending,
  failed, or disconnected before a durable decision was observed — recorded as
  that state, never rounded up to success.

### Handoff disposition (AC-003 / AC-004)

No such capture has been supplied to this engineering run, and none is
performed by this documentation task: no `claude` or `codex` process was
invoked from within this session, no descriptor/socket/capability was created or
used, and no transcript or rollout was read for this purpose. This is the
expected, honest state here — the story's own architecture note excludes
capture from this task ("Capture unavailable now; leave actual two-harness
invocation/correction/decision AC factually unverified").

Consequently:

- **AC-003 remains factually unverified.** The offline aggregate tests in
  `internal/turn/aggregate_lifecycle_test.go` and
  `internal/teamsource/aggregate_instruction_parity_test.go` (T-012.1) establish
  synthetic mechanism coverage only; they are not, and do not claim to be,
  evidence of an actual Claude or actual Codex process invoking this helper.
- **AC-004 is satisfied by this documentation itself:** the missing observation
  is recorded as unavailable, not silently marked satisfied, not fabricated and
  not inferred from a synthetic or historical unrelated capture.
- Only a separately authorized operator can supply the missing evidence, using
  the procedure above, outside any managed engineering run. Nothing in Implement
  or Validate may launch a nested paid agent, widen sandbox permissions, add a
  fallback harness, or otherwise manufacture this observation to close the gap.
- **Validate** is the only mode that reviews whatever capture an operator later
  supplies. Validate reads retained snapshots, checks and captures; it never
  runs or commissions a fresh check to produce this evidence, and it does not
  treat a pending or partial capture as satisfying AC-003.
- The only other route to treating AC-003 as satisfied without a genuine capture
  is an explicit, reasoned, numeric-owner exception (`/waive`; see
  `docs/workflow/owner-exceptions.md`) that names AC-003 exactly and records that
  authentic evidence was absent at the time of the waiver. A waiver changes
  disposition, not the underlying fact; its text must not imply the capture
  happened.

Given both constraints, this feature cannot pass AC-003 through captured
evidence right now, and closing US-012 without either a genuine capture or an
explicit owner exception leaves that criterion open.
