# Discord factory commands

Discord chat is optional and read only. Enable the existing webhook listener and
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
put it in settings. Give the bot permission to send messages in the allowed
channels, plus Read Message History for `/ask` follow-ups. Set the Interactions Endpoint URL in the Discord developer portal to
`https://your-factory-address/litefactory/discord`. The GitHub webhook URL stays
unchanged. Restart the daemon after changing settings. Command registration is
retried on later heartbeats if Discord is unavailable.

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

## Project questions

Run `/ask question:What work is open? project:OWNER/REPO` for a registered
repository. A unique repository name also works. A follow-up `/ask` without a
project uses the newest marked bot answer in the same channel or thread and says
so. Unknown or ambiguous projects get the registered project list without a model
call. Questions are limited to 8192 bytes.

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
