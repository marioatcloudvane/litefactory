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
