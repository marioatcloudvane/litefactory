# Feature Spec: Agent-originated story-creation proposals
# Status: APPROVED
# Date: 2026-09-19

Source of scope: `RESEARCH_BRIEF.md` decisions d1–d13 (owner-agreed) and the
owner's latest comment (a6 = advice only, a7 = register for the graph
Research-node path). No open questions; questions are not permitted this run.

## Goal

A Research agent asked to create issues or stories can originate a creation
proposal itself, so the owner reviews and approves real factory-drafted content
instead of hand-authoring `/mcp-create prepare <JSON>`.

## Verified base facts

The branch has been merged with main, so f14 is now present in this worktree.
Verified against the current code:

| Fact | Evidence |
|---|---|
| Creation wiring in `prepareMCP` is skipped for graph Research-node invocations | `internal/turn/mcp_runtime.go:301` (`req.Invocation != agent.InvocationResearchNode && req.Mode == "research" && … CreationAuthorizer.enabled …`) |
| The issue-comment service is gated the same way | `internal/turn/mcp_runtime.go:292` |
| The graph path calls the same preparer with `Invocation = InvocationResearchNode` | `internal/turn/research_workflow.go:129`, `:139` |
| `CreationPublicationService.Prepare` still has no non-test caller | `internal/turn/creation_publication.go:97` |
| Only the owner comment path retains a proposal | `internal/turn/publication_recovery.go:115-160`, `internal/daemon/mcp_creation.go:46` |
| Owner path nonce is deterministic: `sha256(task:commentID:body)[:16]`; presentation limit 48000 | `internal/turn/publication_recovery.go:152-155` |
| Retaining a proposal makes it current and bumps the revision; an older hash is then unapprovable | `internal/store/mcp_creation.go:118`, `:157-167`, `:291` |
| Envelope shape/limits: 1–5 artifacts, title ≤256, body ≤65536, ≤16 link targets, `linked` ⇒ `parent_issue == 0`, `attach` ⇒ `parent_issue == source issue` | `internal/submission/creation.go:35-52` |
| Reply renderer refuses an envelope over 48000 bytes and rechecks its hash | `internal/report/reporter.go:121-129` |
| Proposal reply body already renders envelope + approval command | `internal/report/reply.go:737-743`, `internal/report/reply.go:1098` |
| Tool catalog is fixed at launch and compared with the installed profile | `internal/turn/mcp_runtime.go:358-365`; catalog version `internal/submission/family_tools.go:10` |
| Family tool specs and mode/creation gating | `internal/submission/family_tools.go:30-84`, `internal/submission/tools.go:99-104` |
| Replies skip when the trigger comment is already consumed (f12) | `internal/turn/report.go:624-631` |
| Delivery identity is `(task, destination, namespace, trigger, run, purpose)` | `internal/turn/report_delivery.go:34-56` |
| A delivery with `RunID != 0` re-finalizes the run on completion; `RunID == 0` only consumes the source | `internal/store/report_deliveries.go:176-184` |
| More than one unresolved delivery for one `(session, trigger)` is a hard error | `internal/store/report_deliveries.go:209-231` |
| Report recovery is keyed `source:<session>:<trigger>`, with prefixed variants (`stopped:`) | `internal/daemon/report_recovery.go:34`, `:126` |
| Both graph and legacy turns end in one place | `internal/daemon/tick.go:1910` `completeTurn` |
| Gate flag | `internal/daemon/setup.go:282` `s.Config.Settings.AllowMCPStoryCreation` |

## User Stories

| ID | Story |
|----|-------|
| US-1 | As an owner who asked Research for issues, I want the agent to draft them, so I review real content instead of writing JSON myself |
| US-2 | As an owner, I want the drafted proposal posted on the issue once after the run ends, with the existing approval command, so approval works exactly as it does today |
| US-3 | As an owner, I want only the last proposal of a run to be approvable, so a superseded draft cannot be published |
| US-4 | As an owner with story creation disabled, I want nothing to change: no new tool, no new comment |
| US-5 | As an owner, I want a proposal whose presentation comment failed to be retried, so a forge outage does not lose the draft |
| US-6 | As an owner, I want the same behaviour whether Research ran on the legacy path or as a graph Research node |
| US-7 | As an agent, I want the tool to tell me the proposal was retained and that the factory will present it, without an approval command I could ever quote |

## Data Model

One new table. Proposals, intents, grants and operations are unchanged (d8).

Migration `internal/store/migrations/0055_creation_presentations.sql`.

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| `mcp_creation_presentations` | `run_id` | INTEGER | yes | PRIMARY KEY; REFERENCES `runs(id)` |
| | `session_id` | INTEGER | yes | REFERENCES `sessions(id)` |
| | `source_task` | TEXT | yes | equals the run's session task ref |
| | `proposal_hash` | TEXT | yes | REFERENCES `mcp_creation_proposals(hash)` |
| | `relationship` | TEXT | yes | CHECK IN (`linked`,`attach`) |
| | `artifacts` | INTEGER | yes | CHECK BETWEEN 1 AND 5 |
| | `retained_ns` | INTEGER | yes | bookkeeping clock, not an argument |
| | `delivery_key` | TEXT | yes | DEFAULT `''`; set when the presentation intent is minted |
| | `presented` | INTEGER | yes | DEFAULT 0; CHECK IN (0,1) |

`PRIMARY KEY(run_id)` is what makes "the latest proposal in a run supersedes
earlier ones" (d7) structural: retention upserts the row for its run.

### Relationships

- `mcp_creation_presentations.run_id` → `runs.id` (one row per run, at most)
- `mcp_creation_presentations.proposal_hash` → `mcp_creation_proposals.hash`
- `mcp_creation_presentations.delivery_key` → `report_deliveries.delivery_key`
- proposal supersession stays in `mcp_creation_current(source_task)` — unchanged

### Store accessors (`internal/store/mcp_creation.go` or a sibling file)

| Accessor | Behaviour |
|---|---|
| `(*CreationStore) RetainRunProposal(ctx, CreationPresentation) error` | Upsert by `run_id` in the same transaction as the existing `RetainProposal`; refuses when the run's session task ≠ `source_task`, or when `presented = 1` already (a presented run does not accept another proposal) |
| `(*DB) PendingCreationPresentation(ctx, runID) (CreationPresentation, error)` | `ErrNotFound` when absent or already presented |
| `(*DB) BindCreationPresentationDelivery(ctx, runID, key) error` | Records the minted delivery key; refuses a different key once set |
| `(*DB) MarkCreationPresented(ctx, runID) error` | Idempotent; requires a bound delivery key |

## API / Interface Changes

### 1. New MCP family tool `issues_create_prepare`

| Item | Value |
|---|---|
| Name | `issues_create_prepare` |
| Registered in | `internal/submission/family_tools.go` `familyTool` + `familyAllowed` |
| Mode | `research` only (`familyAllowed` returns `mode == "research"`; it must **not** consult the `creation` flag, which means "a live grant exists") |
| Gate | Present in the installed list only when `CreationAuthorizer.enabled` and `Creation != nil` (d1, a3) |
| Grant | None. It retains a proposal and publishes nothing (d1) |
| Effect label | `input` |
| Description | "Draft 1–5 issues for the owner to review; retains a proposal only and publishes nothing. Recommend splitting into separate issues when the work looks large." |
| Catalog version | `submission.FamilyCatalogVersion` → `families-v3` |

Input schema (strict, `additionalProperties: false`):

```json
{"type":"object","properties":{
 "relationship":{"type":"string","enum":["linked","attach"]},
 "drafts":{"type":"array","minItems":1,"maxItems":5,"items":{"type":"object","properties":{
   "title":{"type":"string","minLength":1,"maxLength":256},
   "body":{"type":"string","minLength":1},
   "parent_issue":{"type":"integer","minimum":0},
   "link_issues":{"type":"array","maxItems":16,"items":{"type":"integer","minimum":1}}},
  "required":["title","body"],"additionalProperties":false}}},
 "required":["drafts"],"additionalProperties":false}
```

Result on first call (JSON):

```json
{"proposal_hash":"<64 hex>","relationship":"linked","drafts":3,
 "presentation":"The factory will post this proposal on the source issue after this run ends. Retention grants nothing."}
```

| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| tools/call | `issues_create_prepare` | attempt lease + catalog only; no creation grant | Retains a proposal and one `mcp_creation_presentations` row |
| tools/call | `receipts_get({request_id})` | existing | Replay returns the retained receipt, `states.input = "accepted"`, `reference = {kind:"creation_proposal", id:<run_id>}` |
| tools/call | `issues_create_batch` | unchanged | Still requires an owner-approved intent bound to the attempt |
| comment | `/mcp-create prepare <JSON>` | unchanged (d9) | Hand-written owner path retained |
| comment | `/mcp-create <hash> linked\|attach` | unchanged (d9) | 30-minute TTL, intent only (d12) |

### 2. Service layer (`internal/turn`)

| Change | Location |
|---|---|
| `CreationProposalService` (new) wrapping the existing `CreationPublicationService.prepare` with a deterministic nonce and `presentationLimit = 48000`; implements `FamilyOwner` + `Registration()` | new `internal/turn/creation_proposal.go` |
| `PermissionMaterial()` binds `{Enabled, BotID, SourceIssue}` into the catalog digest | same file |
| Registration added in `prepareMCP`, **not** gated on `req.Invocation != agent.InvocationResearchNode` (a7) | `internal/turn/mcp_runtime.go` near `:301` |
| Receipt projection `projections["tool:issues_create_prepare"]` reading `mcp_creation_presentations` | `internal/turn/mcp_runtime.go` near `:241` |
| Guidance line rendered when the tool is installed, carrying the split advice (a6, advice only) | `internal/turn/mcp_guidance.go` |
| `(*Reporter) ReportCreationPresentation(ctx, run store.Run, presentation) (Posted, error)` — mints a `RunID == 0` delivery intent with purpose `creation-proposal:<hash>` and renders through the existing `report.ReplyCreationProposal` body | new `internal/turn/creation_presentation.go` |

### 3. Daemon (`internal/daemon`)

| Change | Location |
|---|---|
| `presentCreationProposal(ctx, t, trigger, turnResult)` called at the end of `completeTurn`, on both the graph branch (after `Consume`) and the legacy branch (after `reportTurn`) | `internal/daemon/tick.go:1910` |
| Retry under the existing report-recovery machinery with recovery key `creation:<session>:<trigger>` (prefix precedent: `stopped:`) | `internal/daemon/report_recovery.go` |

### 4. Prompt/guidance text (Go only)

Split advice ships in the tool description and in `mcpGuidance`. No `.claude/**`
or `.codex/**` file changes, so the protected-tree story split does **not**
apply to this feature.

## Business Rules

**Registration**

- R-1 The tool is installed only when: mode is `research`, the runner supports
  native MCP, `MCPRuntime.DisableMCP` is false, `CreationAuthorizer.enabled` is
  true and `MCPRuntime.Creation != nil`. It is installed for both
  `Invocation == ""` (legacy) and `Invocation == InvocationResearchNode` (graph).
- R-2 Installation implies no grant. `issues_create_batch` registration,
  `families.AllowCreation` and the eligible-intent lookup are unchanged and stay
  gated exactly as today.
- R-3 The tool name is part of the family permission digest before the lease is
  bound, and the installed catalog must equal `spec.PermittedTools()`
  (`mcp_runtime.go:358-365`).

**Input**

- R-4 `relationship` defaults to `linked` (d2).
- R-5 `repository`, `source_task` and `version` are factory-filled and have no
  input field; a payload naming them is refused by the strict schema (d2).
- R-6 With `relationship = "linked"`, `parent_issue` must be absent or `0`. With
  `relationship = "attach"`, it must be absent or equal to the source issue
  number, and the factory fills it with the source issue number. Any other value
  is refused. (Forced by `submission.CanonicalCreation`.)
- R-7 The source issue number is appended to every draft's `link_issues` when
  absent (d5), matching `PrepareCreationProposal`'s behaviour. The post-append
  list must still be ≤16 targets.
- R-8 1–5 drafts (a2). No new quota is introduced; the 20-charged-issues/24h
  repository limit still applies at publication, not at proposal time.

**Retention**

- R-9 The nonce is `sha256(fmt("%s:%d:%s:%s", task, run.ID, attempt, canonicalInput))[:16]`,
  where `canonicalInput` is the tool's exact argument bytes (d3). Identical input
  on the same attempt yields the same marker, the same envelope and the same
  hash; changed input yields a new hash.
- R-10 The 48000-byte cap is applied at call time, via `prepare(..., 48000)`
  over the canonical envelope (d4, f8). Over-size is a refusal, not a truncation.
- R-11 Retention goes through the existing `CreationAuthorizer.RetainProposal`,
  so `mcp_creation_current` supersession and revision bumping are unchanged
  (d7, d9). An identical re-retention does not bump the revision.
- R-12 The tool result never contains an approval command and never names
  `/mcp-create` (d6). The presentation comment does carry the approval command,
  because it reuses the existing reply (d13).
- R-13 The agent proposes only when the owner's request asks for issues or
  stories to be created (d10); this is guidance, not enforcement.
- R-14 The split recommendation is advice in the tool description and the
  agent's report. There is no threshold, no enforcement, and it never blocks a
  proposal (owner's latest comment).

**Presentation**

- R-15 Exactly one presentation comment per run, posted after the run ends,
  showing the last retained proposal of that run (d13).
- R-16 The presentation delivery intent uses `RunID = 0`, the run's
  `TriggerCommentID`, and purpose `creation-proposal:<hash>` — its own delivery
  identity (f12). `RunID` must stay 0 so completion does not re-finalize the run
  (`report_deliveries.go:176-184`).
- R-17 The presentation intent is minted only when no other unresolved delivery
  exists for that `(session, trigger)`; otherwise the debt is left and retried on
  the next tick (`FindPendingReportDelivery` refuses two open purposes).
- R-18 Rendering reuses `report.ReplyCreationProposal` with
  `report.CreationReply{Envelope, Hash, Relationship}`; the renderer's 48000-byte
  and hash checks apply again at render time.
- R-19 `presented = 1` is set only after the delivery is confirmed and completed.
- R-20 A failed presentation records recovery debt under `creation:<session>:<trigger>`
  and is retried with the existing backoff/escalation; the existing
  `MarkReportDeliverySent` + `recoverDelivery` pair prevents a duplicate comment
  (a5, d13).
- R-21 Presentation runs identically for legacy and graph Research turns.

**Unchanged (d9)**

- R-22 Approval syntax, the 30-minute TTL, quotas, `issues_create_batch`
  publication, the hand-written prepare path and `CreationArtifact` (no mode
  field, d11, f6) are untouched. Approval records intent only and launches
  nothing (d12).

## Edge Cases & Error Handling

| Case | Expected behaviour |
|---|---|
| Story creation disabled | Tool absent from the catalog; no bookkeeping row; no presentation comment; nothing else changes |
| Mode ≠ research | Tool absent (`familyAllowed`) |
| Native MCP unsupported or `DisableMCP` | Tool absent; the run proceeds on the shared helper route |
| 0 or >5 drafts | Refused by schema/`CanonicalCreation`; receipt `rejected`; nothing retained |
| Title >256 or body >65536 after redaction/links/marker | Refused ("prepared creation exceeds bounds"); nothing retained |
| Canonical envelope >48000 bytes | Refused with a message to shorten or split; nothing retained (d4) |
| `parent_issue` set with `relationship = "linked"` | Refused; the tool states that `attach` is the only relationship that parents to the source issue |
| `parent_issue` ≠ source issue with `relationship = "attach"` | Refused |
| `link_issues` already contains the source issue | Not duplicated |
| Identical call repeated with a new `request_id` | Same nonce, same hash, same bookkeeping row; revision unchanged; still one comment |
| Identical call replayed with the same `request_id` | Dispatcher returns the retained receipt without re-running the handler |
| Two different proposals in one run | Second upserts the run's row; only its hash is current and approvable; only it is presented (d7, f10) |
| Proposal retained, run then fails | Presentation still happens: the bookkeeping row survives the run outcome |
| Crash after retention, before presentation | Next tick reads `PendingCreationPresentation` for the session's last run and presents it |
| Crash after POST, before confirmation | `PossiblySent` + `recoverDelivery` finds the existing comment by its delivery marker; no second comment |
| Forge outage at presentation | `DeliveryError`; recovery key `creation:…` backs off and retries; session held until due |
| Another unresolved delivery on the same trigger | Presentation deferred to the next tick (R-17) |
| No session row for the task | `RetainProposal` refuses with `ErrCreationAuthority`; receipt `rejected` |
| Owner approves the presented hash within 30 min and comments again | Existing behaviour: the next Research attempt binds the grant and gets `issues_create_batch` (f9, d12) |
| Owner approves a superseded hash | Existing refusal (`mcp_creation.go:160`, `:291`) |
| Repository quota (20/24h) exceeded | Unchanged: refused at `ReserveCreation`, i.e. at publication, not at proposal time |

## Out of Scope

- Mode labels on created issues; the batch format is unchanged (d11, f6)
- Approval launching a run; approval still records intent only (d12, f9)
- Changing the 30-minute window, quotas or the approval syntax (d9)
- Removing or changing the hand-written `/mcp-create prepare` path (d9)
- Any change to `issues_create_batch` publication, reservation or reconciliation
- Issue #314 queue wedging
- Automatic splitting, split thresholds or any enforcement of the split advice
- `.claude/**` and `.codex/**` instruction files (the advice ships in Go)
- Presenting proposals from non-Research modes

## Acceptance Criteria

Tool and catalog

- [ ] AC-1 With `AllowMCPStoryCreation = true`, Research mode and a native-MCP runner, `issues_create_prepare` appears in the installed catalog and in `spec.PermittedTools()`, for `Invocation == ""` **and** for `Invocation == agent.InvocationResearchNode`
- [ ] AC-2 With `AllowMCPStoryCreation = false`, the tool is absent from both paths and no other catalog entry changes
- [ ] AC-3 The tool is absent for `plan`, `implement`, `validate` and `spike`
- [ ] AC-4 Installing the tool alone never registers `issues_create_batch`, never sets `families.AllowCreation` and never binds a creation grant
- [ ] AC-5 The family permission digest changes when the tool is installed, and `FamilyCatalogVersion` is `families-v3`
- [ ] AC-6 `mcpGuidance` renders the creation-proposal line (including the split advice) exactly when the tool is installed

Input validation and retention

- [ ] AC-7 A valid 1–5-draft call retains a proposal, returns hash, relationship and draft count, and the result contains no `/mcp-create` text
- [ ] AC-8 `relationship` omitted ⇒ `linked`
- [ ] AC-9 The source issue appears in every artifact's `link_issues` and in the rendered `Related:` line, exactly once
- [ ] AC-10 `repository`, `source_task` and `version` in the payload are refused by the schema; the retained envelope carries the factory's values
- [ ] AC-11 Refusals: 0 drafts, 6 drafts, blank title, blank body, title >256, body >65536, `link_issues` target <1, >16 targets after auto-link, `parent_issue` with `linked`, wrong `parent_issue` with `attach`. Each leaves no retained proposal and no bookkeeping row
- [ ] AC-12 An envelope over 48000 bytes is refused at call time and nothing is retained
- [ ] AC-13 Identical input on one attempt produces the same hash twice, leaves `mcp_creation_current.revision` unchanged, and leaves one bookkeeping row
- [ ] AC-14 Changed input produces a different hash, bumps the revision, replaces the run's bookkeeping row, and makes the earlier hash unapprovable
- [ ] AC-15 A replayed `request_id` returns the retained receipt without re-running the handler; the receipt projection reports `input = "accepted"` and references the run's presentation row

Presentation

- [ ] AC-16 After a legacy Research run that retained a proposal, exactly one presentation comment is posted, rendering the last retained envelope, its hash and the approval command
- [ ] AC-17 The same holds after a graph Research run whose node retained a proposal
- [ ] AC-18 The presentation delivery has `RunID == 0`, purpose `creation-proposal:<hash>`, and its completion neither re-finalizes the run nor overwrites `runs.result_comment_id`
- [ ] AC-19 A run with no retained proposal posts no presentation comment
- [ ] AC-20 A forge failure at presentation leaves `presented = 0`, records recovery debt under `creation:<session>:<trigger>`, and the next eligible tick posts exactly one comment
- [ ] AC-21 A simulated crash between POST and confirmation results in one comment total: recovery matches the existing comment by delivery marker and confirms it
- [ ] AC-22 A second unresolved delivery on the same `(session, trigger)` defers the presentation instead of minting a second open purpose
- [ ] AC-23 Presenting does not consume a fresh owner comment and does not skip because the run's trigger was already consumed (f12)
- [ ] AC-24 Two proposals in one run produce one comment, showing the later one

Unchanged behaviour

- [ ] AC-25 `/mcp-create prepare <JSON>` still retains and presents a proposal exactly as before
- [ ] AC-26 Approving a presented hash within 30 minutes and commenting again gives the next Research attempt a bound grant and `issues_create_batch`; approving after 30 minutes or approving a superseded hash is refused
- [ ] AC-27 Created issues carry no mode label and `CreationArtifact` gains no field
- [ ] AC-28 `go build ./...` and `make check` pass, including `internal/config/purity_test.go`, `internal/github/surface_test.go` and `internal/turn/gate_test.go`

## Evidence obligations

Recorded here for story authoring to carry and the orchestrator to check before
freeze. `docs/decisions/nested-agent-evidence.md`, referenced by
`.aiteam/APP_CONTEXT.md`, **does not exist in this worktree** — the obligations
below are derived from repository convention (offline Go tests, no integration
tags, `$LF_HOME`-scoped runtime state).

| Criterion | Proof method | Source availability | Bounds / missing prerequisite |
|---|---|---|---|
| AC-1…AC-6 | `internal/turn` + `internal/submission` unit tests over `prepareMCP` / `NewFamilyToolProfile` / `mcpGuidance`, with a fake runner that reports `SupportsMCP() == true` | In-repo, offline | Proves catalog composition only |
| AC-7…AC-15 | `internal/turn` tests calling the registered handler through `NewFamilyToolDispatcher` with a SQLite store, plus `internal/submission` canonical-envelope tests | In-repo, offline | Requires a session row for the task (`RetainProposal` joins `sessions`/`repos`) |
| AC-16…AC-24 | `internal/daemon` + `internal/turn` tests with the existing fake forge/reporter fixtures, following `report_recovery_db_test.go` and `research_failure_publication_test.go` | In-repo, offline | Deterministic clock required; recovery backoff is time-driven |
| AC-25…AC-27 | Existing tests plus additions around `PrepareCreationProposal` and `store/mcp_creation_test.go` | In-repo, offline | — |
| AC-28 | `make check` (fmt, vet, pinned golangci-lint v2, race tests) | In-repo | `dist/litefactory` may be a live daemon; use `go build ./...` |

**Unsatisfiable in-run, therefore not claimed anywhere:**

- That a real Claude or Codex host actually calls `issues_create_prepare` during
  a live Research run. Nested-agent observation is unsupported in the current
  sandbox. The available proof stops at registration, catalog equality, guidance
  text and in-process dispatch. No story may state or imply live host invocation
  as demonstrated; any such claim must be written as an unverified obligation.
- That a real GitHub outage produces the retry path. Proof is a fake forge
  returning errors; it proves the factory's behaviour, not the forge's.
