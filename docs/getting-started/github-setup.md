# GitHub setup

[Getting started](README.md) · [Documentation](../README.md)

Follow these steps in order. At the end litefactory can read your issues, post as
a bot, and is ready for a first Research run. Discord is optional and has its own
page: [Discord](../discord.md).

Litefactory takes instructions from one **owner** and posts as a separate
**bot**. It compares numeric GitHub user IDs, so renaming an account changes
nothing. Using the same account for both is refused.

Before you start, [install litefactory](installation.md).

## Step 1. Log `gh` in as the owner

The owner is you. Litefactory finds the owner by asking the GitHub CLI who is
logged in, so log in to `github.com` with your own account:

```sh
gh auth login
gh auth status
```

The owner needs access to the repository and the right to comment on its issues.
Only this account can direct work. Comments from anyone else are recorded and
ignored.

## Step 2. Create the bot account and token

Create a second GitHub account for the bot (a machine user is fine). Add it to
your repository as a collaborator with write access, and accept the invitation.

Create an access token for that account. A fine-grained token limited to the
repository is the safest choice. It needs, as far as the code shows:

| Use | Access |
| --- | --- |
| Identify the bot | Read the authenticated user. |
| Issue work | Read issues and comments; create comments and reactions; create and update issues, labels and sub-issues. |
| Delivery | Read and create pull requests; merge an approved pull request when repository rules allow it. |
| Webhooks (optional, step 6) | Webhooks: read and write (classic token: `admin:repo_hook`). The account also needs admin access to the repository. |

These requirements come from reading the code, not from a tested list of token
permissions. Organization approval, SSO and branch protection can still block
the bot. Git pushes of task branches use your local Git login, not this token, so
set up Git access to the repository separately.

## Step 3. Create the configuration and store the token

```sh
litefactory init
```

This creates `$LF_HOME` (default `~/.litefactory`), a default `config.yaml` and
the database, and finds the owner from `gh`. It will warn that no bot token is
stored. That is expected. Store the token in the macOS Keychain. The command
asks for the token, so it is never written into a file or shell history:

```sh
security add-generic-password -s litefactory -a github-bot -w
```

The generated `config.yaml` already points at this Keychain item. It holds a
reference only, never the token:

```yaml
providers:
  github:
    token_keychain:
      service: "litefactory"
      account: "github-bot"
```

Run `init` again:

```sh
litefactory init
```

The table should show the owner and the bot as two different accounts. If they
are the same account, init refuses to store them. The daemon will not start until
the bot is resolved. You can use an environment variable instead
(`token_env`), but the Keychain is better for the LaunchAgent, because
`daemon start` copies an environment token into the private plist file. See
[permissions](../configuration/permissions.md) and the
[config.yaml reference](../configuration/config-yaml.md).

Never paste a token into an issue, a comment or a bug report.

## Step 4. Register your repository

Register a local clone of the repository:

```sh
litefactory repo add /absolute/path/to/project
litefactory repo list
```

The clone needs a GitHub `origin` remote. Litefactory reads the owner, name and
base branch from the clone, writes nothing into it, and keeps its task
worktrees elsewhere. Options (`--base`, `--worktrees`, `--prefix`) are in the
[CLI reference](../cli/command-reference.md). Specialist teams are optional; see
the [team commands](../cli/command-reference.md).

## Step 5. Start the daemon

```sh
litefactory daemon start
litefactory daemon status
```

`status` shows whether the LaunchAgent is loaded and when the last tick
finished. Without webhooks the daemon checks each repository by polling once an
hour. See [running the daemon](../operations/running-the-daemon.md) if it does
not start.

## Step 6. Webhooks (optional, recommended)

A webhook lets GitHub tell litefactory about a new issue or comment within
seconds. Without one, litefactory only notices new work at the next poll.

A webhook is only a wake-up call. The daemon never trusts its content; it
re-reads the issue from GitHub. Polling stays on as a safety net, so a missed
delivery is picked up later.

1. Pick how GitHub reaches your Mac (see "Reaching the listener" below).
2. Set `webhook.public_url` in `config.yaml` to the address GitHub will call, for
   example `https://litefactory.example.com/litefactory/webhook`. The daemon
   listens on `webhook.listen` (default `127.0.0.1:8787`) and serves the path
   `/litefactory/webhook`. An empty `webhook.listen` turns the listener off.
3. Restart the daemon after any config change.
4. Register the hook on your repositories:

   ```sh
   litefactory repo webhook --all
   ```

   Use `litefactory repo webhook <owner>/<name>` for one repository. The command
   keeps one webhook subscription for the project's configured ticket
   connection: it repairs the subscription it already owns, imports a hook made
   by an older litefactory, or creates a new one. A new hook uses JSON content,
   the events `issues`, `issue_comment` and `sub_issues`, and the callback
   `public_url` plus an opaque `?subscription=<subscription-id>` selector. Its
   signing secret is kept in the Keychain under service `litefactory-webhook`,
   account `subscription/<subscription-id>`. An imported old hook keeps its
   legacy account `<owner>/<name>`. Running the command again changes nothing if
   the hook is right, and it never touches other hooks. If the project has more
   than one owned subscription for that connection, it stops with `subscription
   selection is ambiguous`; manage it by project and subscription ID (see
   "Inspect and manage a subscription" below). `repo add` registers the hook too
   when `webhook.public_url` is set.
5. Check `litefactory daemon status`. The listener line should say `(bound)`,
   and each repository shows its mode, last delivery, next poll and secret state.

### Reaching the listener

- **Reverse proxy (recommended).** Let a web server you already run end HTTPS and
  forward only the exact path `/litefactory/webhook` to `http://127.0.0.1:8787`,
  with its query string unchanged. Allow `POST` and pass the body and the
  `X-Hub-Signature-256`, `X-GitHub-Event` and `X-GitHub-Delivery` headers
  unchanged. A proxy that drops the `?subscription=` query, rewrites the body or
  drops the signature makes every delivery fail. If the proxy runs in Docker,
  `127.0.0.1` is the container; point it at `host.docker.internal:8787` and check
  with a real delivery.
- **Direct port.** Forward a public port on your router to the Mac and set
  `webhook.listen` to the Mac's LAN address, for example `192.168.1.20:8787`. The
  traffic is then plain HTTP and the daemon is exposed to the internet, so prefer
  the proxy. `repo webhook` warns when `public_url` is plain `http`.

The listener speaks HTTP only. A delivery for an unknown repository, or with a
bad or missing signature, gets a `401` and does nothing. The signature proves who
sent it but hides nothing, so use HTTPS between GitHub and your proxy.

The `?subscription=` selector picks the configured source; the signature still
authenticates the delivery. An old callback without a selector is accepted only
when it maps unambiguously to one source and its cached secret. Delivery
wake-ups and recovery reads go through the project's configured ticket
connection; repository names or issue numbers in the payload never select
another project's work.

To make a hook by hand, use content type `application/json`, the three events
above, the owned subscription's full callback URL including
`?subscription=<subscription-id>`, and the secret from its Keychain item.

### Inspect and manage a subscription

Address a subscription by the registered project ID and subscription ID:

```sh
litefactory repo webhook list <project-id>
litefactory repo webhook status <project-id> [<subscription-id>]
litefactory repo webhook create <project-id> --expected-revision=0
litefactory repo webhook repair <project-id> <subscription-id> --expected-revision=N
litefactory repo webhook update <project-id> <subscription-id> --expected-revision=N
litefactory repo webhook disable <project-id> <subscription-id> --expected-revision=N
```

`create` takes `--expected-revision=0` and fails if the connection already has
an owned subscription. The other changes need the current revision, shown by
`status`; an outdated revision is rejected. `create` and `update` accept
`--callback=<url>`; the selector is added to it. `update` also accepts
`--events` (a comma-separated choice of `issues`, `issue_comment` and
`sub_issues`) and `--enabled=true|false`. `disable` stops webhook delivery and
keeps polling active. Status shows the desired and last observed state, the hook
ID, the last valid delivery and recovery progress. It never prints the signing
secret, which cannot be read back from GitHub.

### Polling settings

`settings.poll_interval_minutes` sets how often each repository is checked. The
default is 60, the range is 1 to 180. Every repository is also checked once when
the daemon starts. The old `poll_interval_seconds` key is no longer accepted; the
daemon refuses to start with it and does not convert the value. Without webhooks,
choose a shorter interval.

### Webhook problems

Run `litefactory daemon status` and match what you see.

| You see | What to do |
| --- | --- |
| `webhook listener  off` | `webhook.listen` is empty. Set it and restart the daemon. |
| `... (not bound)` | The daemon is not running, or the port is taken. Start it or change `webhook.listen`. |
| `mode  polling only` | No `public_url`, or no hook registered. Set it and run `litefactory repo webhook --all`. |
| `mode  webhook, no delivery yet` | A hook exists but nothing has arrived. Check the proxy or port forward, then redeliver a test event from the hook's page on GitHub. |
| `secret  MISSING` | No Keychain secret for that repository's subscription (account `subscription/<id>`, or `<owner>/<name>` for an imported old hook). Run `litefactory repo webhook <owner>/<name>` to repair it, or `litefactory repo webhook status <project-id>` to inspect it, then restart. |

If the log says `poll found a change no webhook announced; check the webhook
path`, a poll found new work that no delivery announced. Look at the hook's
recent deliveries on GitHub for the response code, and check the proxy path and
signature header.

## Step 7. Send your first Research request

Next: [Discord setup](../discord.md) if you want chat (optional), then your
[first Research run](README.md#request-your-first-research-run).

## If something fails

| Feedback | Action |
| --- | --- |
| Owner discovery fails | Run `gh auth status`; log in to `github.com`. |
| Keychain item missing | Store the token under the exact service and account in `config.yaml`, then rerun `init`. |
| Keychain asks for interaction | Unlock the login Keychain in your user session. |
| Bot lookup fails | Check the token, its expiry and network access. |
| API call forbidden | Check that the bot's account, token and organization allow that action. |
| Git push fails | Check your local Git login and branch rules; they are independent of the bot token. |

More: [troubleshooting](../operations/troubleshooting.md).
