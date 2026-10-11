# GitHub webhooks and polling

This guide shows how to let GitHub tell litefactory about new issues and
comments right away, instead of waiting for the next poll.

## 1. What webhooks give you

With a webhook, litefactory reacts to an issue or comment within seconds. GitHub
sends a small notification to the daemon, and the daemon wakes a discovery pass
for that one repository.

Polling stays on as a safety net. A missed delivery (the Mac was asleep, the
network was down) is picked up by the next poll. A webhook delivery is only a
wake-up call: it never carries instructions that litefactory trusts, because the
daemon always re-reads the issue from GitHub.

Delivery and recovery use the project's configured ticket connection. Matching
repository names or ticket numbers on another GitHub API origin do not select
that project's work. Saved sessions retain their original source when a
connection changes.

## 2. Quick start

1. Pick how GitHub reaches your Mac: a reverse proxy (Option A, recommended) or
   a forwarded port (Option B).
2. Set `webhook.public_url` in `config.yaml` to the address GitHub will call,
   for example `https://litefactory.example.com/litefactory/webhook`.
3. Register the webhook on every repository you have added:

   ```
   litefactory repo webhook --all
   ```

   For one repository use `litefactory repo webhook <owner/name>`.
4. Check the result with `litefactory daemon status`. The listener line should
   say `(bound)`, and each repository should show a `mode`, a `last delivery`,
   a `next poll` and `secret  ok`.

The daemon listens on `webhook.listen` (default `127.0.0.1:8787`) and serves the
path `/litefactory/webhook`. Setting `webhook.listen` to an empty string turns
the listener off.

## 3. Option A: reverse proxy (recommended)

Put a web server you already run in front of the listener. It terminates HTTPS
and forwards only the webhook path. This nginx-style snippet is an example; any
proxy that follows the three rules below works.

```
location = /litefactory/webhook {
    limit_except POST { deny all; }
    proxy_pass http://127.0.0.1:8787;
    proxy_pass_request_body on;
    proxy_set_header Host $host;
}
```

The three rules:

1. Match the exact path `/litefactory/webhook` and forward nothing else.
2. Allow `POST`.
3. Pass the body and the `X-Hub-Signature-256`, `X-GitHub-Event` and
   `X-GitHub-Delivery` headers through unchanged. A proxy that rewrites the body
   or drops the signature header makes every delivery fail.

Docker caveat: if the proxy runs in a container, `127.0.0.1` is the container
itself, not your Mac. Point the proxy at `http://host.docker.internal:8787`
instead. The daemon must then accept connections from the container, which may
need a different `webhook.listen` address. That fallback does not work in every
Docker setup, so test it with `litefactory daemon status` and a real delivery.

## 4. Option B: direct port

If you have no proxy, forward a public port on your router straight to the Mac.
For example, forward public port 8443 to the Mac's LAN address and set
`webhook.listen` to that address, such as `192.168.1.20:8787`. Then set
`webhook.public_url` to the public host and port, for example
`http://your.host.example:8443/litefactory/webhook`.

The daemon is then exposed to the internet directly, and the traffic is plain
HTTP. Prefer Option A when you can.

## 5. HTTPS and security

- The listener speaks plain HTTP only. HTTPS ends at your proxy.
- Every delivery is signed with a per-repository secret. A signature stops
  forgery; it does not hide the content. Anyone who can read the traffic can
  read the payload, so use HTTPS between GitHub and your proxy.
- A delivery for an unknown repository, or with a bad or missing signature, gets
  a `401` response and does nothing.
- A valid delivery is only a wake-up. It cannot start work that polling would not
  also find.

When `webhook.public_url` is plain `http`, `litefactory repo webhook` prints a
warning that payloads travel unencrypted.

## 6. Bot permissions

The GitHub account litefactory uses needs admin access to each repository to
create webhooks. For tokens:

- Classic token: the `admin:repo_hook` scope.
- Fine-grained token: the repository permission Webhooks, read and write.

litefactory stores signing secrets in the macOS Keychain under service
`litefactory-webhook`. Qualified subscriptions use account
`subscription/<subscription-id>`; imported legacy subscriptions retain their
proven `<owner>/<name>` account. New subscription callbacks include an opaque
`?subscription=<subscription-id>` selector. Preserve that query when forwarding
the callback. The selector chooses the configured source; the signature still
provides authentication. Legacy callbacks remain supported only when their
verified source is unambiguous. To create a hook by hand, use the owned
subscription's callback and signing secret. Use
content type `application/json` and the events `issues`, `issue_comment` and
`sub_issues`.

Without `webhook.public_url`, registration is skipped and the repository keeps
working on polling alone.

### Inspect and manage a subscription

Use the registered project ID to avoid ambiguous repository shortcuts:

```
litefactory repo webhook list <project-id>
litefactory repo webhook status <project-id> <subscription-id>
litefactory repo webhook create --expected-revision=0 <project-id>
litefactory repo webhook repair --expected-revision=<revision> <project-id> <subscription-id>
litefactory repo webhook update --expected-revision=<revision> --callback=https://new.example.com/litefactory/webhook <project-id> <subscription-id>
litefactory repo webhook disable --expected-revision=<revision> <project-id> <subscription-id>
```

Read the current revision from status before a mutation. An outdated revision is
rejected; inspect again before retrying. Update accepts `--events` with a comma
separated selection of `issues`, `issue_comment` and `sub_issues`, and
`--enabled=false` or `--enabled=true` to set the desired notification state.
Repair preserves an explicit disable.
After a credential edit, compatibility repair and repository registration also
preserve a retained disable for the same verified source. Use explicit creation
for the new connection version if you intend to enable its webhook.

Status separates the desired state from the last observed remote state. It also
shows the source version, hook ID, check time, last valid delivery, completed
recovery coverage and enrollment boundary. A pending, failed or uncertain remote
operation is not proof that GitHub accepted the change. Signing secrets are not
printed, and GitHub cannot return a stored secret for equality verification.

If a creation response is lost, repair first searches for the exact owned
callback and configuration. Ambiguous or incomplete evidence leaves the operation
unresolved rather than creating another hook or changing an unrelated hook.
Subscription changes are trusted local operator operations; ticket comments and
agents cannot manage them.

## 7. Polling without webhooks

Webhooks are recommended. If you cannot use them, set a shorter poll interval.

- `settings.poll_interval_minutes` sets how often each repository is checked. The
  default is 60. The minimum is 1 and the maximum is 180.
- Each repository's next poll is due one interval after its last completed poll,
  regardless of webhook deliveries.
- Every repository is checked once when the daemon starts.
- The old `poll_interval_seconds` key is refused (see section 9).

Recovery progress and pending ticket observations are saved in the database.
A poll advances completed coverage only after both ticket changes and reply
changes have been handed off durably. Interrupted or incomplete pages are
retried from retained progress after restart. Overlapping polls, repeated
deliveries and reconciliation all use the existing session consumption and
confirmed-report records, so observing an already consumed reply does not run
its instruction again. An interrupted first ticket instruction also remains
pending until processing finishes or the usual admission rules skip it.

Routine GitHub discovery overlaps the last completed coverage by five minutes.
Paced reconciliation also revisits enrolled tickets and replies to recover
updates that appeared late in GitHub's index. Replies and unchanged ticket bodies
from before enrollment stay historical; eligible replies received during an
outage remain eligible when the ticket is first discovered later. Recovery
requires the provider to retain the change and access to become available again.

Disabling a webhook subscription prevents its notifications from admitting new
work. Polling and saved work continue; webhook disable does not disable
the project's ticket connection.

## 8. Troubleshooting

Run `litefactory daemon status` and match what you see.

| You see | Likely cause | What to do |
|---|---|---|
| `webhook listener  off` | `webhook.listen` is empty. | Set it, for example `127.0.0.1:8787`, and restart the daemon. |
| `webhook listener  <addr>/litefactory/webhook (not bound)` | The daemon is not running or the port could not be bound. | Start the daemon; if the port is taken, change `webhook.listen`. |
| `mode  polling only` | The listener is off, or no hook is registered and nothing arrived. | Set `webhook.public_url` and run `litefactory repo webhook --all`. |
| `mode  webhook, no delivery yet` | A hook is registered but nothing has arrived since the daemon started. | Check the proxy or port forward, then redeliver a test event from the hook's page on GitHub. |
| `mode  webhook` | A valid delivery arrived since start. | Nothing to fix. |
| `last delivery  none since start` | No valid delivery since the daemon started. | Same as `webhook, no delivery yet`. |
| `last poll  none since start` | No poll has completed since the daemon started. | The first poll is due immediately; if this persists, check daemon logs for listing failures, rate limits or discovery budget deferrals. Webhooks do not postpone polls. |
| `secret  MISSING` | The listener is on but the Keychain has no secret for that repository. | Run `litefactory repo webhook <owner/name>` to create it, then restart. |

If the daemon log shows the warning

```
poll found a change no webhook announced; check the webhook path
```

a poll found new work that no delivery announced. Typical causes: the hook was
never registered or was deleted, the proxy drops the signature header or changes
the path, GitHub cannot reach `webhook.public_url`, or the delivery was rejected
with `401`. Look at the hook's recent deliveries on GitHub to see the response
code, then fix the proxy or run `litefactory repo webhook --all`.

## 9. Upgrading from poll_interval_seconds

Older configs used `poll_interval_seconds`. That key is no longer supported and
is not converted: the daemon refuses to start and tells you to set
`settings.poll_interval_minutes` (1 to 180) instead. Remove the old key, choose a
value in minutes, then follow the quick start above.
