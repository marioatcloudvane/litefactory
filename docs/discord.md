# Discord

[Documentation](README.md) · [Getting started](getting-started/README.md)

Discord chat is optional. It lets the owner ask about factory state, preview and
create feature issues, diagnose a stuck issue, answer Research questions, read
usage, and receive task notices. Chat cannot start, approve, stop or repair
work, and nothing typed in Discord reaches the GitHub comment parser.

Set up GitHub first, including its webhook listener: [GitHub setup](getting-started/github-setup.md).
For Linear tickets, also complete [Linear setup](project-connections.md#linear-tickets-with-github-code).

## Set up

### 1. Create the Discord application and bot

1. In the [Discord developer portal](https://discord.com/developers/applications), create an application. Copy its
   **Application ID** and its **Public Key** (64 hex characters).
2. Add a bot to the application and copy the **bot token**.
3. Under the bot settings, turn on **Message Content Intent**. It is needed for
   ordinary (natural language) messages. The factory checks the application
   flags and a channel history read; if either fails, ordinary messages are
   switched off and chat health reports it.
4. Under Installation, configure Guild Install with `bot` and
   `applications.commands` scopes. Open the install link, choose your test server
   and authorize it. Give the bot, in each channel you will allow:
   View Channel, Send Messages, Attach Files and Read Message History.

### 2. Find the numeric IDs

Turn on Developer Mode in Discord, then right-click to copy:

- your own **user ID**: the owner;
- each **channel ID** where the factory may talk: the allowlist.

A thread needs its own entry in the allowlist.

### 3. Store the bot token

Keep the token out of `config.yaml`. In Terminal.app or an SSH shell able to
unlock the login Keychain, as the daemon user, paste the bot token at `read` and
press Return. Do not enable shell tracing:

```sh
security unlock-keychain ~/Library/Keychains/login.keychain-db
read -rs T; security add-generic-password -s litefactory-discord -a bot -w "$T"; unset T
```

The chosen service `litefactory-discord` and account `bot` match the config
below. Use `add-generic-password -U` for an existing item. Alternatively, use an
environment reference; Keychain suits the LaunchAgent without embedding tokens
in its plist.

### 4. Add the chat settings

Chat needs the webhook listener on, with a public **HTTPS** address
(`webhook.listen` and `webhook.public_url`). Then add to `$LF_HOME/config.yaml` (default `~/.litefactory/config.yaml`).
Replace each placeholder with the value collected above; IDs must be numeric
strings and the public key must be the application's 64-character hex key:

```yaml
chat:
  discord:
    enabled: true
    application_id: "APPLICATION_ID"
    public_key: "PUBLIC_KEY_HEX"
    token_keychain: {service: litefactory-discord, account: bot}
    # or: token_env: DISCORD_BOT_TOKEN   (use exactly one of the two)
    owner_id: "OWNER_USER_ID"
    channel_ids: ["CHANNEL_ID"]
    notice_channel_id: ""    # optional, must be one of channel_ids
```

| Key | Meaning |
| --- | --- |
| `enabled` | Turns chat on. |
| `application_id` | Numeric Discord application ID. |
| `public_key` | Ed25519 public key in hex; used to check Discord's requests. |
| `token_keychain` / `token_env` | Reference to the bot token. Exactly one. |
| `owner_id` | Numeric user ID of the only person the factory obeys. |
| `channel_ids` | Allowed channels (at least one, unique, numeric). |
| `notice_channel_id` | Channel for task notices. Empty means no notices. |

Unknown keys and literal tokens are startup errors. The full key table is in the
[config.yaml reference](configuration/config-yaml.md).

### 5. Point Discord at the factory

Your proxy must forward `/litefactory/webhook` and `/litefactory/discord` to
`127.0.0.1:8787`. Use the [nginx example](getting-started/github-setup.md#reaching-the-listener);
preserve the raw body and Discord's `X-Signature-Ed25519` and
`X-Signature-Timestamp` headers. Use your public HTTPS hostname.

Load the config and start the listener **before** saving the portal endpoint:

```sh
litefactory daemon stop
litefactory daemon start
litefactory daemon status
```

Confirm the listener is bound and inspect the `chat` line. In the developer
portal's General Information, set **Interactions Endpoint URL** to
`https://factory.example.com/litefactory/discord` (replace the host). Saving it
must pass Discord's signed endpoint verification. See the
[Discord setup guide](https://docs.discord.com/developers/quick-start/getting-started)
for portal installation and endpoint verification.

Slash commands are registered with Discord; if Discord is
unavailable, registration is retried later. If the bot token cannot be read, the
daemon still starts with chat unavailable. It logs an error naming the token
setting, `litefactory daemon status` shows the reason on its `chat` line, and the
token is tried again on later heartbeats, so storing it later works without a
restart.

### 6. Check it

As the owner, in an allowed channel, run `/help`, `/health` and `/status`. Each
should give one answer. Another user, or a command from a channel not on the
list, gets only a private "no access" reply.

## Slash commands

All commands are owner-only and work only in allowed channels.

| Command | What it does |
| --- | --- |
| `/help`, `/health`, `/status` | List commands; read factory health and status. |
| `/ask question:... [project:OWNER/REPO]` | Ask about a project and its open work. Question up to 8192 bytes. A follow-up without `project` uses the newest bot answer in the same channel or thread. |
| `/details` | Show raw details of the previous reply; ordinary “show details” is also supported. |
| `/draft [project:...] [note:...]` | Preview a feature issue (note up to 2000 bytes). Shows project, title, body and an 8-character code. Creates nothing. |
| `/confirm code:CODE` | Create the previewed issue. Valid for 24 hours. A new `/draft` replaces the old preview and code. Confirming twice returns the same issue. |
| `/diagnose issue:N [project:...] [note:...]` | Explain a stuck issue. Never repairs or posts to the issue. |
| `/question issue:N [project:...]` | Show the current Research question and its 12-character token. |
| `/answer issue:N revision:TOKEN text:... [project:...]` | Answer that Research question (up to 65536 bytes, redacted). An old token or closed issue is refused. A repeat says `already answered`. The answer is copied once to the issue as a factory note. |
| `/meter [project:] [task:] [mode:] [agent:] [model:] [from:] [to:] [details:] [page:]` | Usage, cost estimates and coverage. Dates are UTC (`YYYY-MM-DD` or RFC3339); `from` is inclusive, `to` exclusive. Paging commands are in the reply footer. |
| `/prices [agent:] [model:] [page:]` | Read the recorded price catalogue (USD per million tokens). |

`project` can be `OWNER/REPO`, or just the repository name when it is unique. For
`/question`, `/answer` and `/diagnose` it may be left out when only one project is
registered. `/answer` only works for published Research questions; answer
reviews and approvals on the issue. These commands cannot change prices or
accounting settings. Details: [usage and costs](meter.md).

`/ask` answers come from fresh open issues and current task data, cite the
facts used and give an as-of time. They use the doctor's isolation gate with all
tools off; if no provider is verified, answers are unavailable. At most the
latest 20 bot `/ask` turns (32 KiB) are used as history.

## Natural language

The owner can also write ordinary messages in an allowed channel. Slash commands
stay available.

- Reads run once the action and target are clear. If something is missing or
  ambiguous, the bot asks a question. Unsupported requests do nothing.
- A change (such as drafting, creating an issue, diagnosing or answering) always
  needs a separate confirmation. The bot first shows the exact action, target and
  content, with a confirmation code. Reply directly to that bot message with
  `yes`, or send `confirm <code>`. A casual "yes", a quote, or a "yes" with extra
  instructions does not count. A proposal expires after 15 minutes.
- Send a correction as a new message; a changed proposal gets a new code. Send
  `cancel` to cancel. Editing or deleting your message is not cancellation, and
  an action that has already started cannot be undone.
- Only newly sent owner text counts. Bots, webhook messages, system messages,
  attachments, embeds and forwarded messages cannot give requests. Old history is
  not replayed when chat is first enabled, and requests older than 24 hours are
  not run.

Existing review and approval gates still apply to everything chat starts.

## English examples

Replace `OWNER/REPO` with the registered repository and `N` with the issue/task
number shown by the factory.

- `/ask question:What is waiting for my review? project:OWNER/REPO`
- `/details`, or `show details` after the answer.
- `Draft a feature for OWNER/REPO to add a keyboard shortcut for search.`
  Read the proposed action, then send `confirm <code>` with its displayed code,
  or reply directly to that proposal with `yes`. If a further creation preview
  is returned, confirm that exact preview before expecting an issue.
- `/draft project:OWNER/REPO note:Add a keyboard shortcut for search`, followed
  by `/confirm code:CODE` using the returned draft code.
- `/question issue:N project:OWNER/REPO`, then
  `/answer issue:N revision:TOKEN text:Use the existing search index. project:OWNER/REPO`
  using the returned Research token.
- `/meter project:OWNER/REPO` and `/diagnose issue:N project:OWNER/REPO`.

Verify the resulting issue link or Research answer receipt. A preview alone
creates nothing; approvals for subsequent work still belong on the ticket.

## Task notices

Set `notice_channel_id` to one of your allowed channels to get one message when a
task starts or changes mode, when it waits for you (a question or review), and
when it finishes, fails or stops. Notices name the issue number and title and
link to it. A waiting notice is sent once per wait. For a question it shows a
shortened version; answer on the issue (or with `/answer`). Final notices include
available usage and estimated cost. On first enable, existing history is skipped,
and events already over 24 hours old are skipped and logged.

## Reading replies

A reply has a short explanation checked against the full result. Exact values,
links, codes and paging commands stay unchanged, and the original redacted result
is attached as `raw-details.txt`. The reply language follows your current `/ask`
question or the latest `/ask` turn in the channel or thread, otherwise English.
If the explanation fails its checks, the reply is marked raw and still has the
details.

## Limits

- **Best-effort delivery.** Replies and notices are retried for up to 24 hours.
  Discord checks duplicate-send tokens for only a few minutes, so after a
  restart or delayed retry a reply can appear twice. Exactly-once is not
  promised.
- **Little is stored.** Interaction tokens stay in memory. Finished reply text is
  dropped and records are purged after 24 hours. Chat history is not stored.
- **Message size.** Replies are limited to 2000 characters per message; long
  content is shortened (token and codes are kept) and the full text is attached
  or on the issue.
- **One owner, fixed channels.** No one else can use chat. Threads need their own
  allowlist entry.
- **No work control.** Chat cannot start, approve, stop or repair work.
- **Untested against a live server.** Automated tests use a fake Discord. Your
  own channel permissions, Message Content setting and provider isolation are
  worth checking yourself.

## If it does not work

| Symptom | Check |
| --- | --- |
| `daemon status` chat line says the token cannot be resolved | Store the token under the Keychain service/account (or env variable) in the config. |
| `User interaction is not allowed` | Unlock the login Keychain in Terminal.app or an interactive SSH shell as the daemon user, then retry storing the token. |
| Portal endpoint verification fails | Start the configured listener first; check public HTTPS, exact `/litefactory/discord` path, raw body, signature headers and the application public key. |
| Config refused at startup | `chat.discord` needs `webhook.listen`, an HTTPS `webhook.public_url`, numeric IDs, a 64-hex public key and exactly one token reference. |
| Commands do not appear | Restart the daemon; check the Interactions Endpoint URL and that the proxy forwards `/litefactory/discord`. |
| Plain messages ignored | Enable Message Content; give the bot channel visibility and history; check `/health`. |
| "no access" | Wrong user, or the channel is not in `channel_ids`. |

See also [owner commands](guides/owner-commands.md) for the GitHub comment commands.
