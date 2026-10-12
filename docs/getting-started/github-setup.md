# GitHub setup

[Getting started](README.md) · [Documentation](../README.md) · [Linear setup](../project-connections.md#linear-tickets-with-github-code)

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
every repository it will publish to as a collaborator with **Write** access, and
accept each invitation while signed in as the bot. Private repositories the bot
cannot see can return `inspect repository` / `404`; check the accepted invitation
and token repository selection before assuming the repository does not exist.

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
reads a hidden paste, so the token is not saved in shell history. Run this in
Terminal.app or an SSH shell able to unlock your login Keychain, as the same
macOS user that runs the daemon. Tools without an interactive login session can
fail with `User interaction is not allowed`. Unlock first; paste the token at
`read` and press Return (do not enable shell tracing):

```sh
security unlock-keychain ~/Library/Keychains/login.keychain-db
read -rs T; security add-generic-password -s litefactory -a github-bot -w "$T"; unset T
```

The service `litefactory` and account `github-bot` below match the generated
configuration. If replacing an existing item, use `add-generic-password -U`
with the same service/account.

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
[CLI reference](../cli/command-reference.md).

Appoint the human ticket approver before creating a test issue. Replace
`PROJECT_ID` with the local numeric ID from `repo list`, `HUMAN_LOGIN` with your
GitHub login, and `CONNECTION_REVISION` with the current ticket revision from
`connection list` (the displayed `version`, not its internal `id`):

```sh
litefactory repo connection list PROJECT_ID ticket
litefactory repo connection check PROJECT_ID ticket
litefactory repo approver inspect PROJECT_ID
litefactory repo approver set PROJECT_ID HUMAN_LOGIN --connection-revision CONNECTION_REVISION --revision 0
litefactory repo approver inspect PROJECT_ID
```

Use `set --revision 0` only if no appointment exists. For an existing appointment
of the same person, use `repo approver refresh PROJECT_ID --connection-revision
CONNECTION_REVISION --revision APPROVER_REVISION`, taking the approver revision
from `inspect`. Verify the human identity and absence of authority blockers.
`ticket approver must differ from the publishing account` means the publisher's
credential belongs to the intended approver; use the separate bot token.

An installed team **must be linked before any mode runs**. Choose `TEAM_NAME`
from the catalogue configured by `settings.team_source`; install its latest
stable release, then copy `TEAM_VERSION` from `team list`:

```sh
litefactory team install TEAM_NAME
litefactory team list
litefactory team inspect TEAM_NAME TEAM_VERSION
litefactory team link TEAM_NAME TEAM_VERSION --repo /absolute/path/to/project
```

Expect `Installed` (or `Already installed`) and `Linked` (or `Already linked`).
Use the same clone path as `repo add`. Inspect the team's stack profile: its
executables, OS, architecture and checks must fit the repository. A repository
may supply `.litefactory/stack.overrides.json` with `"version": 1` and only the
`guidance`, `checks`, `setup`, or `environment`
fields allowed by that profile's `override_fields`. Overrides replace those
fields; they cannot change host requirements or permissions or clear required
fields. Overrides must be in the admitted Git revision; a dirty working file
does not affect a run. See [stack profiles](../stack-profiles.md) for the profile format.

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
3. Reload configuration with `litefactory daemon stop`, then
   `litefactory daemon start`; confirm `litefactory daemon status`.
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
   and each repository shows `mode  webhook` and `secret  ok`. In GitHub repository
   Settings → Webhooks → the hook → Recent Deliveries, generate a signed issue
   or comment event and verify HTTP **202**, not **401**. A ping alone does not
   prove issue dispatch; check the last delivery and a real test issue too.

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

For nginx on the same host, add these locations inside your existing HTTPS
`server` block (use your domain and certificate in that server's TLS settings):

```nginx
location = /litefactory/webhook {
    proxy_pass http://127.0.0.1:8787;
    proxy_set_header Host $host;
}
location = /litefactory/discord {
    proxy_pass http://127.0.0.1:8787;
    proxy_set_header Host $host;
}
```

A `proxy_pass` without a URI preserves the original path and query string;
default nginx forwarding keeps request headers and body. Validate your proxy
configuration with `nginx -t`, then reload it through your installation's service
manager. The Discord location is used by [Discord setup](../discord.md).

The listener speaks HTTP only. A delivery for an unknown repository, or with a
bad or missing signature, gets a `401` and does nothing. The signature proves who
sent it but hides nothing, so use HTTPS between GitHub and your proxy.

The `?subscription=` selector picks the configured source; the signature still
authenticates the delivery. An old callback without a selector is accepted only
when it maps unambiguously to one source and its cached secret. Delivery
wake-ups and recovery reads go through the project's configured ticket
connection; repository names or issue numbers in the payload never select
another project's work.

A hand-made hook (including one made with `gh api`) is adopted only when the
original ticket source is verified, the legacy signing secret already exists in
Keychain service `litefactory-webhook`, account `OWNER/REPO`, and exactly one
remote hook matches the configured callback URL. An arbitrary hook is not
adopted automatically. Zero or multiple exact matches block legacy recovery;
inspect the hooks and retained subscription before changing anything.

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
| `User interaction is not allowed` | Unlock the login Keychain in Terminal.app or an interactive SSH shell; repeat the hidden paste as the daemon user. |
| `ticket approver must differ from the publishing account` | Store the bot token and appoint the human. |
| `inspect repository` / `404` | Accept the bot invitation for this repository and check token access. |
| Webhook delivery `401` | Preserve query, raw body and signature headers; inspect the subscription and its Keychain secret. |
| Bot lookup fails | Check the token, its expiry and network access. |
| API call forbidden | Check that the bot's account, token and organization allow that action. |
| Git push fails | Check your local Git login and branch rules; they are independent of the bot token. |

More: [troubleshooting](../operations/troubleshooting.md).
