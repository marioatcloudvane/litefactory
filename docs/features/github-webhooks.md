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

litefactory stores each repository's secret in the macOS Keychain under service
`litefactory-webhook` and account `<owner>/<name>`. To create a hook by hand,
read that secret from the Keychain and enter it as the hook's secret. Use
content type `application/json` and the events `issues`, `issue_comment` and
`sub_issues`, with the payload URL set to your `webhook.public_url`.

Without `webhook.public_url`, registration is skipped and the repository keeps
working on polling alone.

## 7. Polling without webhooks

Webhooks are recommended. If you cannot use them, set a shorter poll interval.

- `settings.poll_interval_minutes` sets how often each repository is checked. The
  default is 60. The minimum is 1 and the maximum is 180.
- A delivery for a repository postpones that repository's next poll.
- Every repository is checked once when the daemon starts.
- The old `poll_interval_seconds` key is refused (see section 9).

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
| `next poll` far in the future, `last poll  none since start` | Webhooks postponed the poll, or the daemon just started. | Expected while deliveries arrive. |
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
