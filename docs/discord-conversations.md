# Persistent Discord conversations

An allowed channel can opt into one named conversation. Ordinary owner messages
then go exclusively to that conversation. Unbound channels do not read ordinary
messages; `/details` is the only slash command.
The bot still needs Message Content, channel visibility and message history access.

```yaml
chat:
  discord:
    # Keep the existing application, owner, credential reference and webhook settings.
    channel_ids: ["123456789"]
    conversations:
      - channel_id: "123456789"
        name: "Owner work"
        provider: claude # or codex; configure that provider's balanced model
```

Configuration is loaded at daemon startup. Renaming a conversation preserves its
native session. A provider change waits for admitted turns, replies, live proposals,
asynchronous action outcomes and the intake backlog to drain, then creates a
separate binding and native directory. Provider
context is never transferred. The previous provider must remain configured while
its work drains. Removing a binding pauses retained work; it does not reinterpret
queued conversation messages as factory commands.

The same ongoing agent answers ordinary factory questions and follow-ups through
factory-managed lookup requests. Factory code checks enabled commands, current
channel access and registered targets, then supplies bounded redacted results as
prompt context. Lookups cover help, health, status, usage, prices, project facts
and current research questions when those commands are enabled. The agent gets
no repository, database, credentials or native tools. Factory facts and coverage
gaps are retained privately with the answer; remembered facts are not fresh
lookup evidence.

Supported changes include feature preview and creation, diagnosis, Research answers,
standards authoring and publication, exceptions and log analysis. Every change
uses the same fixed impact policy as the remote owner connector. Reads require
no confirmation. The factory freezes the normalized arguments, registered scope,
exact target revision, owner and client provenance, timestamps and estimate (or
explicit unknown) before review. The agent cannot change those facts at confirmation.

Say yes separately at least five seconds after the exact proposal has been
successfully delivered. Expiry is fifteen minutes from creation, including
preparation and delivery time. No early yes is queued or automatically retried.
Binding actions then require the configured owner's normal Unicode ✅ on the
specific factory approval message before expiry. Details and the complete tier
catalogue are in [shared action confirmation](discord.md#confirming-an-action).

Use plain words to inspect, cancel or select a proposal; the client and factory
carry its identifiers. A bare yes selects only one current delivered proposal.
A changed request creates a fresh fixed proposal; stale targets and changed
permissions require a fresh review. Cancellation wins only before admission and
cannot undo admitted work. Request `show that` for the recorded status and result,
or `show history` for owner-only history references. History is retained for
ninety days from terminal completion, independently of normal chat cleanup;
active and unresolved recovery identities remain available longer.

Missing exact owner content or unclear targets require clarification. Exact
owner-supplied quoted arguments are data: byte spans preserve whitespace and
literal escapes inside delimiter quotes. Quoted, forwarded and fenced content
cannot confirm a proposal. Admission, executing, succeeded, failed and uncertain
are separate facts; an uncertain effect is inspected through retained domain
receipts and never blindly repeated.

Each ordinary turn allows at most three lookups and four provider invocations,
including up to two response corrections across the whole owner turn. A consult
permits the existing fifth invocation. Corrections consume the same call budget,
not extra calls, and successful decoding or a lookup does not reset their count.
The exchange journal remains bounded to 16 records; work stops when its required
records or any existing size, lookup or call budget cannot fit. Defect feedback
and the next correction prompt are protected and saved before launch.
All continuations use the selected native
session. Saved action outcomes survive explanation and delivery failures; uncertain
provider input and effects are not replayed. A completed asynchronous diagnosis is
supplied at the next eligible owner turn. Internal lookup exchanges are not sent
to Discord. Conversation calls remain in the factory-wide usage ledger with
coverage gaps because the current adapters expose no usage measurements. Token
and cost amounts are not reconstructed. The factory supplies redacted prompt fields and fixed instructions
every turn. Each adapter has its own isolation gate. Unverified capabilities refuse
launch; the workflow and diagnosis gates are unchanged. See
[recorded capability evidence](conversation-evidence.md) for this installation's
limits. Help output alone does not demonstrate native recall or sandbox isolation.

The factory database and private provider files under its `conversations` directory
must survive on the same installation. Sessions resume by the saved exact native
identity, never by the latest session. Native compaction retains useful context,
not necessarily every early detail. Each turn receives the fixed instructions again.

Input is saved before launch and completed output before sending. A restarted
factory waits for the launch lease and establishes that its old provider process
has exited before reclaiming fenced ownership. A complete, validated correction
boundary can continue with its exact saved prompt and native session. The factory
rebuilds the remaining correction, call and lookup budgets and checks current
access, references, targets and captured instruction identity before continuing.
An incomplete boundary or uncertain provider input is never replayed. A saved
session can then continue with the next owner message. Missing session
identity or files produce a visible pause rather than a replacement conversation.
A health lookup reports durable conversation health reasons. A removed owner
or channel and permanent Discord refusal preserve queued work and saved answers.
Paused state needs explicit administrative resolution; restarting alone does not
clear a permanent refusal or invent a lost native session. Saved action outcomes
are reconciled first and reported even when correction or recovery cannot
continue. Neither a saved action nor an uncertain effect is repeated to improve
its narration. Budget, persistence, redaction and recovery failures report the
limitation and retain known outcomes and uncertainty. This covers ordinary
process restart with retained storage, not storage loss or exactly-once effects.

Within each channel, later turns wait until the predecessor's reply is acknowledged.
Temporary delivery failures retry the exact saved bytes with the same nonce and
honor Retry-After while other channels remain eligible. Discord's nonce protection
lasts only a few minutes: delayed retries may deliver duplicate copies. Local
ordered dispatch does not guarantee exactly-once remote delivery or the ordering
of a late remote completion.

Admitted pending turns and replies do not expire after 24 hours. Redundant prompt snapshots from delivered completed native answers
and delivered reply bodies are purged on the existing retention schedule.
Interrupted and unavailable prompt snapshots remain for recovery; native state,
source identity, sequence and delivery metadata remain. Discovery of previously
unadmitted messages still uses the first-enable boundary and 24-hour window.
Native state has a fixed 1 GiB ceiling with a 64 MiB free-disk reserve.
Queues are bounded at 1,000 pending messages/turns per channel, and storage errors
stop admission or launch rather than discarding work. Owner input is refused when
it exceeds the intake byte budget; it is never silently truncated. Oversized saved
answers remain retained if Discord refuses their protected delivery; long-answer
presentation belongs to the dependent story.

## Grounding and saved answer details

Each factory conversation answer carries one opaque answer reference across all
numbered parts. Use `/details answer:<reference>` to select it. Omit the selector
to choose the latest answer with at least one acknowledged part in your current
owner/channel scope; that selection is fixed when the command is admitted. An
answer with no reads reports that no tool results were obtained. Unknown,
unauthorized and expired references never substitute another answer's evidence.

Free explanation remains provider wording; associating it with a source does
not establish that its paraphrase is accurate. Source identity, target and
caveats remain attached to each current fact. Prior conversation claims,
matching numbers and provider assertions do not establish grounding.

Bounded valid plain prose can be retained as an answer when no protocol object
or apparent malformed protocol payload is present. Structured and action-shaped
output still needs strict validation; fallback prose never invokes a command or
becomes evidence. Unknown fact references keep the answer's useful wording with
a visible factory warning. Incomplete-source caveats remain beside the text and
in saved delivery parts. A reference association alone does not verify a
paraphrase.

New conversations capture the current versioned instruction package. An open
turn keeps its retained instruction bytes and identity across restart. Quiescent
bindings refresh through the existing capture rules; updating installed sources
never rewrites a saved continuation prompt. The separately pinned legacy
conversation package remains unchanged.

Details exports the saved redacted results with result identities, normalized
arguments, targets, capture times, statuses, completeness and gaps. It does not
include native transcripts, prompts or private reasoning, and makes no fresh
lookup or model call. Current channel and registered target access are rechecked
before sending. Ordered exports use `raw-details.txt` attachments no larger than
64 KiB; their manifest identifies byte continuations. A definite upload refusal
switches remaining exports to saved numbered inline text. Uncertain delivery
retries the same saved payload.

Answers preserve accepted content in order, within Discord's 2000 UTF-16-unit
message budget, with repeated warnings and selectors. An entire proposal remains
one unchanged protected block; confirmation binds only to its acknowledged
message. Parts are saved before delivery, and the next part waits for durable
acknowledgment. Failed delivery resumes saved bytes after restart; later
conversation answers wait. Remote duplicates remain possible. Permanent refusal
or access loss preserves pending parts and reports a channel health reason.
Completed answer details expire 24 hours after the entire answer is delivered;
pending answer evidence and pending exports survive outages.

## Standards in the owner conversation

Ask what standards operations are available, then ask to start or inspect a saved
standard draft. Authoring questions have saved session, version and question
references. Answers and edits use a preview and a separate confirmation; answering
a question never approves the resulting proposal. The same saved work is visible
through the standards CLI.

Ask to see the review of one exact proposal version to read its before and after.
Approving it starts an action proposal: the factory shows the exact proposal id,
version and digest. After reviewing it, say yes separately after the minimum
five-second delay, then tap ✅ on the factory's separate approval request before
expiry. Both confirmations authorize exactly that reviewed version. Publication
requires a separate action proposal with the same conversational confirmation and
Discord ✅ steps for the approved proposal. Its result identifies the published
book revision. The factory carries the identifiers; no confirmation code is
needed.

After an interrupted reply, inspect the saved operation or draft before trying
again. A committed operation can succeed even when delivery fails; recovery uses
its original receipt. Interrupted generation requires explicit continuation.
Operation receipts survive ordinary restart and delivered chat-body cleanup. Missing references never silently select the latest proposal.
SQLite uses NORMAL synchronization: this recovery covers ordinary process
interruption, not power loss, storage loss or administrator changes.

Ask to analyze a project's run logs, for example "analyze acme/api logs for the
last 7 days". The factory proposes the exact project and UTC window; your
confirmation starts the analysis in the background. One analysis per project runs
at a time. When it finishes, its summary, proposed standards and findings are
posted in the conversation with the analysis ID, and findings show whether they
were filed. Ask for a dry run to see what would be filed without filing anything.
A failure posts its reason.
Proposed standards stay unapproved until you review them. Ask to list recent
analyses or show one by ID. An analysis interrupted by a restart is recorded as
incomplete and is not resumed. Analyses never start by themselves.

## Policy rollout and rollback

The policy migration invalidates old pending confirmations with a request to
propose them again. Existing admitted, executing and uncertain executions retain
their original invocation and recovery journal; they are not relabelled as newly
consented work. Existing read grants stay read-only. Request fresh explicit
selected-repository action consent or separate installation-wide standards
management consent before using MCP changes. Token refresh cannot add authority.

Run one policy-aware daemon version against the migrated database. Rolling back
must not reactivate invalidated proposals or run an old daemon alongside the new
one. Preserve the database and receipt records when investigating uncertain work.
Old captured conversation instructions cannot bypass runtime policy; quiescent
conversations obtain updated guidance through the existing capture refresh.
