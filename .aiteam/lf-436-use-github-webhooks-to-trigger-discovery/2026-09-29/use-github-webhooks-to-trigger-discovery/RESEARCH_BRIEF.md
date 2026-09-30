# Research brief

## Request

Add GitHub webhook delivery to wake the daemon; delivery path (tunnel) is the factory owner's concern; explain how to make the mini's public IP work with port 80 taken by Docker and how LiteFactory picks hooks up; keep polling as a configurable fallback, default 60 min, range 1-180 min; a received webhook postpones the poll for the set interval.

## Decisions

- **d1** — Tunnel/ingress is out of LiteFactory's scope; it only documents the expected local listener and URL path.
- **d2** — One webhook secret per repo, held in the keychain.
- **d3** — Listener is plain HTTP, localhost by default, port 8787, path /litefactory/webhook, POST only; TLS ends at the owner's proxy.
- **d4** — Owner decision: fallback poll is in minutes (default 60, range 1-180) and replaces the seconds key; the old key is rejected with a pointer to the new one. Webhooks are the recommended setup; short polls serve setups without one.
- **d5** — Accepted events: issues, issue_comment, sub_issues; a valid delivery only triggers a scoped tick, payload never trusted as state.
- **d6** — Duplicate deliveries dropped by X-GitHub-Delivery in an in-memory window; start-up reconcile covers restarts.
- **d7** — Registration via repo add and a repair command needs an owner-configured public URL; without it the repo runs on polling only.
- **d8** — daemon status shows listener address, last delivery and last poll per repo, and next poll due; a log line flags polls that find changes no webhook announced. Adaptive interval is out of scope.
- **d9** — Owner decision: a valid webhook for a repo restarts that repo's poll countdown, so the poll runs only after a full interval with no webhook. Per-repo timer; rejected deliveries do not postpone; a start-up reconcile always runs.


## Assumptions

- **a1** — The listener binds to a configurable address, default localhost on a non-80 port, because Docker owns port 80 on the owner's mini; the owner forwards to it via reverse proxy or port forward.
- **a2** — HTTPS is terminated by the owner's proxy; the daemon serves plain HTTP and relies on HMAC signature verification.
- **a3** — Migration of the old seconds key is a hard error with guidance, not silent conversion.
- **a4** — Full reconcile also runs at daemon start, as the issue proposes.
