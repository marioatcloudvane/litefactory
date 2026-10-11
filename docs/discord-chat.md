# Discord factory commands

Discord chat is optional. Commands read factory state, except `/answer`, which
supplies an answer to an existing Research question. Enable the existing webhook listener and
set its public HTTPS GitHub endpoint (for example,
`https://your-factory-address/litefactory/webhook`). Keep that endpoint unchanged
when adding the Discord route. Your tunnel or proxy must forward both paths to
the same listener. Add `chat.discord` to the factory settings:

```yaml
chat:
  discord:
    enabled: true
    application_id: "YOUR_NUMERIC_APPLICATION_ID"
    public_key: "YOUR_64_CHARACTER_HEX_PUBLIC_KEY"
    token_keychain:
      service: litefactory-discord
      account: bot
    # Alternatively, replace token_keychain with token_env: DISCORD_BOT_TOKEN
    owner_id: "YOUR_NUMERIC_USER_ID"
    channel_ids: ["YOUR_NUMERIC_CHANNEL_ID"]
```

Store the bot token in the referenced keychain item or environment variable; never
put it in settings. Give the bot Send Messages and Attach Files permissions in the allowed
channels, plus Read Message History for `/ask` follow-ups and reply language selection. Set the Interactions Endpoint URL in the Discord developer portal to
`https://your-factory-address/litefactory/discord`. The GitHub webhook URL stays
unchanged. Restart the daemon after changing settings. Command registration is
retried on later heartbeats if Discord is unavailable. If the bot token cannot be
resolved, the daemon still starts with chat unavailable: it logs an error naming
`chat.discord.token_keychain` or `chat.discord.token_env`, `daemon status` shows
the reason on its `chat` line, and the token is resolved again on later
heartbeats, so storing it later starts chat without a restart.

Run `/help`, `/health` and `/status` as the configured owner in an allowed channel.
Each should show one answer. Another user or a command from another channel should
see only a private `no access` response. No incoming chat content reaches the
factory's GitHub comment parser.

Replies are admitted once per interaction and retried for up to 24 hours. Tokens
stay in memory. Restarted or expired interactions use channel replies with stable
enforced nonces. **Delivery is best effort**, as approved for option 2: Discord
checks nonce uniqueness only for a few minutes. A response lost after Discord
accepted a send, followed by a restart or delayed retry, can produce a duplicate.
There is no guarantee of exactly one visible reply in those uncertain cases.
Confirmed and failed replies lose their answer text; terminal records are purged
24 hours later. No chat history or interaction tokens are stored.

## Issue diagnosis and Research answers

Run `/diagnose project:OWNER/REPO issue:NUMBER` to explain a stuck issue. A unique
repository name also works; the issue number must be positive. An optional `note`
adds context. Diagnosis waits for the existing doctor slot and returns findings,
coverage gaps and open questions. It never applies repairs or posts to the issue.
Interrupted diagnoses get one restart retry. Optional notes stay in memory and
are omitted after a restart.

Run `/question project:OWNER/REPO issue:NUMBER` on a task waiting for a Research
answer. Copy its 12-character token into
`/answer project:OWNER/REPO issue:NUMBER revision:TOKEN text:YOUR_ANSWER`.
Only published Research questions accept answers. Reviews and approvals must be
answered on the issue. An old token or a closed issue is refused; the current
question and token are shown when available. Answers are limited to 65536 bytes,
redacted, and applied once. Long observations and questions are marked as
shortened to fit Discord; the revision token is always preserved, and the full
question remains on the issue. The existing run continues through its normal
dispatch and the answer is copied once to the issue as a factory note. Chat
cannot start, approve, stop or repair work.

For the real-server walkthrough, diagnose a stuck issue and check the reply.
Then show and answer a waiting Research question, check that the run continues
and the issue contains the note, and repeat the answer. The repeat should say
`already answered`. Raw answer text is kept only in memory until admission to the
wait; an interrupted, unrecorded answer must be sent again by the owner.

## Project questions

Run `/ask question:What work is open? project:OWNER/REPO` for a registered
repository. A unique repository name also works. A follow-up `/ask` without a
project uses the newest marked bot answer in the same channel or thread and says
so. Unknown or ambiguous projects get the registered project list without generating a project answer. Questions are limited to 8192 bytes.

Answers use fresh open issues and current factory task metadata. Each point cites
a checked fact and the reply gives an as-of time; omissions and unavailable
history are shown. Only the latest 20 bot-authored `/ask` turns (up to 32 KiB) are
used. Ordinary messages are not included. Asking cannot create or change work.
The configured providers use the doctor's isolation gate, with all tools disabled
and the balanced model tier. If none is verified, answers are unavailable.
Temporary question and project text is cleared after confirmed or failed delivery.

For a real-server check, ask about open work, then ask a follow-up without naming
the project. Check source links and the project carry-over notice. Ask about an
unknown project and check that the registered project list appears.

## Meter walkthrough

As the configured owner in an allowed channel, run `/meter` for the whole factory,
then `/meter project:OWNER/REPO` for a registered project and
`/meter project:OWNER/REPO task:ISSUE` for one task. If the same project name is
registered on more than one forge, use `project:github:OWNER/REPO`.

Try a time window, for example
`/meter project:OWNER/REPO from:2026-10-01 to:2026-10-10`. Dates mean UTC midnight;
from includes that instant and to excludes it. Full RFC3339 times work too.
Optional mode, agent and model arguments narrow the existing report further.

Run `/meter project:OWNER/REPO details:true` and copy the next-page command from
the reply footer. Each command returns one page and reads the report again. To
keep the time window fixed while paging, set `to` to the footer's as-of time.
Check that the estimate label, collection and pricing status, known token
subtotals, price revisions and overhead exclusion are readable. Partial or
unpriced usage must say that the total is unknown.

Run `/prices`, then `/prices agent:codex` and `/prices agent:codex model:MODEL`
to inspect the existing price catalogue. Long catalogues offer the same next-page
command; `/prices page:2` reads the catalogue again. These commands only read reports;
prices and accounting settings cannot be changed through chat. A report that
exceeds existing snapshot limits asks you to narrow the project, task or time
filter. Confirm these results in your real server as the owner walkthrough;
the automated fake Discord test does not establish real-server readability.

To receive task status notices, set `chat.discord.notice_channel_id` to one of
`channel_ids`. Leave it empty to disable notices. Each event goes to that one
channel: a task starting work or moving to a different mode (for example
Research → Plan), owner question, completion, failure or stop. Steps between
graph nodes inside one mode post nothing, and a further run in the same mode,
such as a run recovered after a daemon restart, does not post another start.
Messages name and link to the task; questions link
to the issue comment and show a shortened question. Answer on the issue as usual.
Final notices include available per-run usage and estimated costs.

On first enable, existing history is skipped. Unfinished runs and unpublished
questions are revisited when they finish or are published. Events already more
than 24 hours old when first observed are skipped and logged. Notices use the
same approved best-effort channel-post delivery as command replies, with stable
enforced nonces, growing retry waits and rate-limit hints, for up to 24 hours.
The nonce window limitation described above also applies to notices. Confirmed
and failed notices lose their text and are purged 24 hours later; only source
read positions and unresolved record references remain.

## Reading command replies

Command replies use a short explanation checked against the completed result.
Exact values, links, revision codes, command examples and paging commands stay
unchanged. The same reply includes `raw-details.txt` with the original redacted
result. For `/meter` and `/prices`, these details are the page you requested.

Replies follow the language of your current `/ask` question, or the latest
marked `/ask` turn in the same channel or thread. When the language or history
cannot be determined, they use English. Earlier answers are not given to the
reply narrator.

If narration is unavailable, times out or fails its checks, the reply is marked
as raw and still includes the original details. If Discord definitely rejects
the attachment, the saved raw page is retried inline. Delivery retries reuse the
saved result; they do not repeat a completed command or completed narration.

Authentication and malformed interaction responses remain immediate. Duplicate
`/answer` acknowledgements use a small raw reply without applying the answer
again. Automated status notices keep their existing wording.
