# Discord

[Documentation](README.md) · [Getting started](getting-started/README.md)

Discord chat is optional. In a channel bound to a conversation, the owner writes
ordinary messages to ask about factory state, preview and create feature issues,
diagnose a stuck issue, answer Research questions, manage shared standards and read usage. A notice
channel can receive task notices. Chat cannot start, approve, stop or repair
work, and nothing typed in Discord reaches the GitHub comment parser.

Set up GitHub first, including its webhook listener: [GitHub setup](getting-started/github-setup.md).
For Linear tickets, also complete [Linear setup](project-connections.md#linear-tickets-with-github-code).

## Set up

### 1. Create the Discord application and bot

1. In the [Discord developer portal](https://discord.com/developers/applications), create an application. Copy its
   **Application ID** and its **Public Key** (64 hex characters).
2. Add a bot to the application and copy the **bot token**.
3. Under the bot settings, turn on **Message Content Intent**. It is needed for
   ordinary conversation messages. Owner messages arrive over the Discord
   Gateway, an outbound WebSocket, as they are sent. The factory also subscribes
   to guild message reactions for binding action approval; without Message Content
   Discord closes it (4014) and the `chat` line of `litefactory daemon status`
   says so. The factory also checks the application flags and a channel
   history read; if either fails, ordinary messages are switched off and chat
   health reports it.
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
    conversations:
      - channel_id: "CHANNEL_ID"
        name: "Owner work"
        provider: claude       # or codex
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
| `conversations` | Binds an allowed channel to one named conversation and its provider. See [persistent conversations](discord-conversations.md). |

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

The `/details` command is registered with Discord; it replaces any older
command set. If Discord is unavailable, registration is retried later. If the bot token cannot be read, the
daemon still starts with chat unavailable. It logs an error naming the token
setting, `litefactory daemon status` shows the reason on its `chat` line, and the
token is tried again on later heartbeats, so storing it later works without a
restart.

### 6. Check it

As the owner, write a message such as `What is the factory health?` in a bound
channel. The conversation should answer. Another user, or a channel not on the
list, gets no answer; `/details` from them gets only a private "no access" reply.

## Using chat

Only the owner can use chat, and only in allowed channels.

- **Ordinary messages** in a channel bound under `conversations` go to that
  channel's conversation. It reads factory state, project facts, usage and
  Research questions, and proposes changes (feature draft and issue creation,
  diagnosis, Research answers) that need a separate confirmation. Allowed
  channels without a binding do not read ordinary messages. Details:
  [persistent conversations](discord-conversations.md).
- **`/details [answer:]`** is the only slash command. It shows the saved raw
  tool results for a conversation answer reference, or for the latest
  acknowledged answer in the channel. Without a saved answer it says that no
  details are available. Other slash commands that a Discord client may still
  show are refused with a private note.

Existing review and approval gates still apply to everything chat starts.
Usage and costs are described in [usage and costs](meter.md).

## Confirming an action

Discord and the [remote owner connector](guides/remote-connector.md) use the same
proposal policy. Ask in ordinary words, review the exact factory block, then say
`yes` separately at least five seconds after confirmed proposal delivery. A
proposal expires fifteen minutes after creation. An early yes does nothing; no
confirmation retry is queued. You can say `cancel`, `show that` or `show history`
without copying the history reference. Reply directly to the proposal when other
messages have intervened; an ambiguous yes never selects an action.

Feature preview creation (`confirm`), standard publication, proposal approval and
exception approval or revocation also need your normal Unicode ✅ reaction on the
factory's separate approval request. The original conversational yes comes first.
Only your configured Discord account, the exact returned factory message and an
observed reaction before expiry count. Custom emoji, bots, another message or
another person cannot approve it. With several configured destinations, an MCP
client must select one of them.

A missing or failed Discord send leaves the proposal unexecuted. A known rate
limit can retry within the original expiry; a send with an uncertain outcome is
stopped, without blind replay. After a missed Gateway session, tap again before
expiry or request a fresh proposal. Never assume an unobserved reaction happened.

The full tier catalogue, fresh action consent and recovery rules are in the
[owner connector guide](guides/remote-connector.md#plain-word-owner-actions).

## Task notices

Set `notice_channel_id` to one of your allowed channels to get one message when a
task starts or changes mode, when it waits for you (a question or review), and
when it finishes, fails or stops. Notices name the issue number and title and
link to it. A waiting notice is sent once per wait. For a question it shows a
shortened version; answer on the issue or in the conversation. Final notices include
available usage and estimated cost. On first enable, existing history is skipped,
and events already over 24 hours old are skipped and logged.

## Limits

- **Best-effort delivery.** Replies and notices are retried for up to 24 hours.
  Discord checks duplicate-send tokens for only a few minutes, so after a
  restart or delayed retry a reply can appear twice. Exactly-once is not
  promised.
- **Little is stored.** Interaction tokens stay in memory. Finished reply text is
  dropped and ordinary replay records are purged after 24 hours. Action review
  facts, timed decisions, reaction evidence and recorded outcomes have separate
  owner-only history for ninety days after terminal completion. Active actions
  and unresolved uncertain effects retain their recovery identities longer.
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
| `/details` does not appear | Restart the daemon; check the Interactions Endpoint URL and that the proxy forwards `/litefactory/discord`. |
| Plain messages ignored | Bind the channel under `conversations`; enable Message Content; give the bot channel visibility and history; check the `chat` line of `litefactory daemon status`. |
| `chat` line says the Gateway is reconnecting or failed | Allow outbound HTTPS/WebSocket to `gateway.discord.gg`; for `disallowed intents (4014)` enable Message Content Intent in the developer portal and restart. Meanwhile messages are read every 5 minutes, and all missed messages are read once the Gateway reconnects. |
| "no access" | Wrong user, or the channel is not in `channel_ids`. |

See also [owner commands](guides/owner-commands.md) for the GitHub comment commands.
