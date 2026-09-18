# MCP tool families

The #273 family APIs extend the [MCP adapter](mcp-adapter.md) with validation,
issue discovery and publication, workflow checkpoints, advisory activity and
request recovery. They are callable through the existing bound broker and MCP
stdio server when the factory installs their handlers. Claude/Codex native integrations are implemented; #264 composes the production
owners. Authentic working/Validate observations remain outstanding on #274/#296.

Preflight checks proposed input before submission. Activity describes reported
progress. Receipts recover what the factory knows happened to a request. None of
these three operations approves scope or turns a status message into evidence.

## Tool visibility and arguments

This matrix describes a fully installed `families-v2` catalog. Every discovery
and call requires a live matching attempt. An uninstalled handler is unavailable.
Direct calls enforce the same permissions as discovery.

| Tool | Research | Implement | Validate | Plan / Spike |
| --- | --- | --- | --- | --- |
| `communication_preflight`, `communication_submit`, `communication_receipt` | Yes | Yes | Yes | Existing behavior |
| `feedback_preflight` | Yes | Yes | Yes | No |
| `issues_list`, `issues_get`, `issues_comment` | Yes | Yes | Yes | No |
| `issues_create_batch` | Creation gate and live grant | No | No | No |
| `evidence_submit` | No | Engineering / legacy checkpoint | Assessment | No |
| `activity_update`, `activity_get` | Yes | Yes | Yes | Yes |
| `receipts_get` | Yes | Yes | Yes | No |

The creation gate controls discovery; a visible creation tool still requires a
valid factory-held grant to execute. Ordinary comments use explicit target
authorization regardless of the creation setting. Reading an issue never adds
it to the authorized comment targets.

MCP `tools/call` takes `{"name":"tool_name","arguments":{...}}`. The following
table describes the arguments object; unknown fields are refused. Repository,
run, attempt and permission choices are factory-owned. Creation includes its
reviewed repository/source fields for exact-content verification, and these must
match the binding.

| Tool | Arguments | Result and constraints |
| --- | --- | --- |
| `feedback_preflight` | `{"kind":"application\|checkpoint\|creation","input":{...}}` | Shared validation diagnostics. Application uses `AgentResultService.Preflight`; checkpoint uses `SubmissionSession` through `EvidenceService`; creation uses `CanonicalCreation`. Checkpoint preflight is available only in Implement/Validate. No domain submission, snapshot capture, retained proposal, grant or publication; submission validates again. |
| `issues_list` | `{"state":"open","page":1,"per_page":30,"include_mode_labels":false}`; all optional | One page of `items`, `page`, `per_page`, `next_page`. State is open/closed, default open; page defaults 1 and must be positive; per_page defaults 30 and must be 1–100. |
| `issues_get` | `{"issue_number":123,"state":"open","include_mode_labels":false}` | One summary; positive repository-local number required, state defaults open, state mismatch refused. |
| `issues_comment` | `{"issue_number":123,"message":"Progress details"}` | Durable family receipt, with confirmed comment ID when known. Requires an explicitly bound existing issue; PRs are refused. Nonblank valid UTF-8 message and final redacted body including marker must fit the 65,536-byte comment limit. |
| `issues_create_batch` | Exact prepared `CreationEnvelope`, described below | Durable operation reference and confirmed artifact identities/relationship states when known. Never accepts a caller token, grant, approval sentence or hash instead of the envelope. |
| `evidence_submit` | Existing typed `agent.Contribution` checkpoint JSON | Shared checkpoint receipt, applying workflow transitions rather than merely storing JSON. See [story protocol](story-contract.md). |
| `activity_update` | `{"sequence":1,"message":"Checking recovery"}` | Reducer disposition (`accepted`, `replayed`, `stale`, `rejected`), optional code and current advisory view. |
| `activity_get` | `{}` | Current advisory view: `available`, `unavailable` or `cleared`, bounded counters, optional latest message and terminal timestamp. |
| `receipts_get` | `{"request_id":"retained-request-id"}` | One authorized retained request outcome. Does not create a receipt for this lookup or invoke the original handler. |

Issue summaries contain only `number`, `title` and optional recognized
`mode_labels`. Bodies, comments, account data, PRs and arbitrary labels are
excluded. Listing fetches one bounded forge page: a PR-only page can return an
empty `items` array with a nonzero `next_page`. It does not scan ahead to fill a
page. `next_page: 0` means there is no reported next page.

Evidence preserves frozen story/revision checks, immutable snapshot/check
references, permitted capture/invalidation and the existing persisted workflow
version. Implement can record engineering completion without acceptance. Validate
uses retained evidence and cannot capture snapshots or execute tests through the
tool. Historical accepted snapshots remain intact; no checkpoint selects the next
mode automatically.

Activity requires a positive sequence and nonblank valid UTF-8 message of at most
2 KiB. The existing reducer enforces 1,024 accepted updates, 32 retained replay
identities and terminal retention of at most 24 hours using factory-supplied time.
Calls are synchronized and returned mutable values are copied. A durable update
receipt can survive loss of the process-local display; querying it does not
reapply the message or renew any deadline.

## Request identity and recovery

`submission.Client.Call` preserves exact argument bytes. Before transmitting a
family call, the client journal persists a stable request ID namespaced by tool
and private descriptor identity; the private directory also isolates attempts.
MCP JSON-RPC IDs remain connection-local and are not durable operation IDs.
Identical retries using the same descriptor recover the original correlation.
Different argument bytes can create a different request identity; callers should
retain the original bytes and response `request_id` when a reply is uncertain.

The family ledger stores bounded metadata and references to authoritative records,
not full issue catalogs or authored read/preflight payloads. Replaying an admitted
family identity returns its receipt rather than rerunning its handler. Creation
replays additionally recheck live grant authority. A fresh read that needs a new
catalog response must use a distinct request payload/correlation; exact replay is
request recovery, not a refresh.

| Receipt field | Meaning |
| --- | --- |
| `request_id`, `tool` | Stable request correlation and family tool, when retained. |
| `disposition` | `missing`, `rejected`, `pending`, `uncertain` or `confirmed`. Confirmed bookkeeping for a read/preflight does not imply workflow approval. |
| `reference` | Applicable authoritative record kind, numeric ID or underlying request ID. |
| `states` | Separate `input`, `engineering`, `acceptance`, `effects` dimensions where applicable; absent dimensions are not inferred successes. |
| `publications` | Up to five observed issue IDs/numbers, comment IDs and relationship outcomes. Unknown identities are not invented. |

Authenticated malformed or denied calls may retain a rejected disposition; a
transport failure before durable admission can leave no record. `pending` means
the outcome has not been established; `uncertain` preserves ambiguous effects.
Receipt lookup cannot settle an uncertain remote write by retrying it.

A request ID is not a read capability. The current live reader must have the
exact current or explicitly approved historical scope. A separately authorized
current reader may inspect permitted history after old authority is revoked;
this does not reactivate the old lease or grant. Receipts exclude descriptor
secrets, private permits and unrelated payloads.

Comments persist final redacted bytes and their correlation marker before durable
dispatch reservation. Concurrent retries and reopen cannot obtain another POST
for that dispatched operation. Factory `ReconcileComment` observes one bounded
page and accepts only the exact target, numeric bot identity, marker and prepared
content. Ambiguous or incomplete observations remain uncertain and never permit
blind reposting.

## Exact creation preparation and publication

Production owner control (selected Research mode, loaded creation gate enabled):

1. The authenticated numeric owner comments `/mcp-create prepare <CreationEnvelope JSON>`.
   Repository/source are factory-bound; omitted relationship defaults to `linked`.
   Preparation adds the source issue link, redacts title/body, retains the complete
   envelope and returns its exact JSON and SHA256 in a durable report. Prepared
   JSON must fit the 48,000-byte exact presentation limit. Preparation grants nothing.
2. After reviewing those exact bytes, the owner separately comments
   `/mcp-create <SHA256> linked|attach`. Edited controls, mismatched content or
   relationship, absent gate and non-Research mode refuse admission.
3. The next eligible native Research launch consumes the unexpired intent once.
   Its catalog digest includes that intent and proposal hash; launch context
   supplies the exact approved envelope. Resume/replacement cannot inherit a
   consumed creation grant. A launch without native MCP does not consume it.

The production runtime independently installs ordinary comment publication for
Research, Implement and Validate with the current issue as its explicit target.
Other issue targets are refused. Comments do not require the creation gate.

The daemon observes at most one pending comment page and one pending creation
page per minute, traversing open/closed issue pages and retained operations.
Restart loses only traversal cursors: durable uncertain intents remain pending
and observations repeat safely. Recovery never redispatches writes, resumes a
partial batch or invents success when no exact numeric-identity match is found.


Factory-only `CreationPublicationService.Prepare` binds repository/source,
defaults the relationship to `linked`, adds links and correlation markers,
redacts final bytes, validates and retains the proposal. It returns an envelope
and canonical hash for review without minting a grant or publishing. Its envelope
has this shape (values are illustrative; callers use the exact returned bytes):

```json
{
  "version": 1,
  "repository": "owner/repository",
  "source_task": "github:owner/repository#123",
  "relationship": "linked",
  "artifacts": [
    {"title": "Reviewed title", "body": "Exact prepared body including marker", "link_issues": [123]}
  ]
}
```

Canonical validation requires 1–5 artifacts, nonblank valid UTF-8 titles of
1–256 bytes and bodies of 1–65,536 bytes after preparation, and at most 16 positive
link targets per artifact. `linked` has no parent. `attach` requires each
`parent_issue` to equal the source issue and must be explicitly reviewed.
Any prepared-content or relationship change needs a fresh proposal and approval.

`settings.allow_mcp_story_creation` remains default off. Omitted/false settings
must be passed through registration unchanged. Enabling it grants no permission
by itself. The existing `CreationAuthorizer` owns trusted owner provenance,
approval intake, grant binding, reservation and the private single-use dispatch
permit. Grants are server-held; GitHub prose, copied commands, labels, comment
volume and tool arguments cannot substitute for them. No permit is serializable
into the tool protocol.

The primitive limits each attempt to one batch/five artifacts and each repository
to twenty artifacts per rolling 24 hours. Approval expires after 30 minutes,
bounded by the attempt lease. Expired authority refuses creation replay as well
as new dispatch. The canonical hash binds reviewed content; grant-scoped
reservation identifies the operation. Neither is the client journal identity.

Publication uses `ReserveCreation`, `BeginDispatch`, `RecordOperationOutcome`
and `ReconcileOperation` as the authority. It records per-artifact and relationship
outcomes and stops further dispatch after a failure or unknown result. Partial or
uncertain operations keep their quota charges and are not reported as a fully
confirmed batch. Factory `Reconcile` performs bounded observation under separate
authorization; it does not mint a permit or recreate issues. These local dispatch
guarantees do not establish remote exactly-once delivery.

## Factory composition and #274 handoff

Construct the family owners with one matching `store.ResultBinding`, migrated
store and trusted factory dependencies. Configure `ReceiptService` with explicit
historical scopes and authoritative projections for checkpoints, comments and
creation. Compose `FeedbackService` and, for Implement/Validate, `EvidenceService`.
Add query, publication and activity registrations through
`FamilyDispatcherOptions.Registrations`; pass loaded creation policy and explicit
comment targets. `FamilyPermissionDigest` must be computed with the same options
before binding the lease. `NewFamilyToolDispatcher` refuses inconsistent or
missing dependencies and mismatched immutable permission material. Creation's
`BindGuard` attaches that matching guard before execution.

The digest binds catalog version, installed handlers, application scope, creation
visibility, comment targets and historical receipt permissions. Every invocation
passes the live `AttemptGuard` before handler access; mutable creation authority
is checked again by its existing owner. A stale, replaced, cancelled or expired
attempt cannot regain access through discovery or replay.

The existing communication helper contract remains compatible: communication
submit retains its original journal identity, communication preflight remains
nonjournaled, and communication receipt observes existing submission correlation.
MCP still refuses publishable/story-bearing Plan proposals; ordinary helper Plan
policy is unchanged. Plan/Spike gain only activity reads/updates; unrelated
family tools remain unavailable.

| Historical #274 responsibility | Current implementation and evidence boundary |
| --- | --- |
| Native launches | Implemented: `internal/turn/mcp_runtime.go` binds actual invocations; `internal/harness/mcp.go` and `internal/codex/mcp.go` configure native launches. Claude strict configuration excludes ambient registrations; Codex uses scoped overrides and permitted tools. Authentic connection remains unverified. |
| Production scope construction | Implemented in the production MCP runtime: attempt bindings, current-issue comment target, scoped receipts, digests, broker/guard and eligible creation service. |
| Trusted approval intake | Implemented by daemon creation intake and runtime binding: exact preparation, numeric-owner approval and one eligible launch. Grants remain factory-held. |
| Activity lifecycle | Implemented factory lifecycle and daemon cleanup supply terminal/clear inputs. They cannot extend lease, grant or terminal deadlines. |
| Recovery scheduling | Implemented bounded daemon reconciliation observes comments/creation under separate authority; it preserves uncertainty without restoring dispatch authority. Live remote outcomes still require observation. |
| Authentic verification | Observe both actual hosts discovering/calling permitted families, direct refusals, isolation, cancellation, reconnect and recovery, with retained evidence of actual host behavior. |

Earlier foundation notes assigned launch/control wiring to #264 and a subsequent
#274 handoff. Those milestone assignments are historical: the production and native
connections above now exist. Authentic #274/#296 host observations remain outstanding. Normal Go pipe/socket/SQLite/HTTP fixtures and the aggregate `make check`
gate establish local implementation behavior only. Their execution evidence is
retained with the engineering session; this document makes no authentic host or
live GitHub publication claim.

## Advisory lifetime after adoption

Production registers both activity operations with the same catalog and digest
permissions before exposing the attempt. Success, failure, cancellation,
interruption, replacement and expiry revoke obsolete capabilities. The bounded
registry is cleaned by daemon-owned scheduling, including before remote backoff;
shutdown clears abandoned owners. The current policy clears terminal display
immediately. Progress is process-local and grants no application, engineering or
acceptance completion. Cleanup preserves durable receipts and uncertain publication
intent for separate daemon reconciliation. No authentic host observation follows
from these offline source/lifecycle assertions.

## Attachments and user guidance

Issue attachments are ingested by `internal/turn/prepare.go` through `internal/attach`,
subject to mode permission and fetch capability. They are not an MCP file-transfer
family. Images and raw PDF/DOCX/PPTX documents have type/count/byte limits;
rejection does not by itself fail the run, and ingestion does not guarantee
interpretation. See [attachments](guides/attachments.md), [factory MCP](guides/mcp.md)
and [Using the factory](guides/README.md).
