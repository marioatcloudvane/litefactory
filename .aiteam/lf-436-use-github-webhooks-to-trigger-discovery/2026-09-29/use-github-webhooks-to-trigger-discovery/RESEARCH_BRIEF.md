# Research brief

## Request

Add GitHub webhook delivery to wake the daemon; delivery path (tunnel) is the factory owner's concern; explain how to make the mini's public IP work with port 80 taken by Docker and how LiteFactory picks hooks up; keep polling as a configurable fallback, default 60 min, range 20-180 min.

## Decisions

- **d1** — Tunnel/ingress is out of LiteFactory's scope; it only documents the expected local listener and URL path.


## Assumptions

- **a1** — The listener binds to a configurable address, default localhost on a non-80 port, because Docker owns port 80 on the owner's mini; the owner forwards to it via reverse proxy or port forward.
- **a2** — HTTPS is terminated by the owner's proxy; the daemon serves plain HTTP and relies on HMAC signature verification.
- **a3** — The fallback poll is configured in minutes (default 60, range 20-180) and replaces the seconds-based setting; migration of the old key is for shaping.
- **a4** — Full reconcile also runs at daemon start, as the issue proposes.
