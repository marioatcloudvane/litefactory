# Persistent Discord conversations

An allowed channel can opt into one named conversation. Ordinary owner messages
then go exclusively to that conversation; slash commands and confirmation controls
keep their existing behavior. Unbound channels keep their ordinary-message route.
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

Supported changes remain draft, feature confirmation, diagnosis and research
answers. The factory renders the exact action, target and protected owner content
with a 15-minute proposal code. Only a later owner reply to the delivered proposal
or its exact code can authorize it. Cancellation, replacement, expiry and repeat
confirmation cannot run it again. A draft only prepares a feature preview, which
still requires separate confirmation. Missing exact owner content or unclear
targets require clarification. Slash commands, including `/details`, retain their
existing behavior.

Each ordinary turn allows at most three lookups and four provider invocations,
including one response correction. All continuations use the selected native
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
has exited before recording an interruption. Uncertain input is never replayed.
A saved session can then continue with the next owner message. Missing session
identity or files produce a visible pause rather than a replacement conversation.
The `/health` command reports durable conversation health reasons. A removed owner
or channel and permanent Discord refusal preserve queued work and saved answers.
Paused state needs explicit administrative resolution; restarting alone does not
clear a permanent refusal or invent a lost native session.

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
