---
name: research-mode-integration
description: Research mode for work with an external system, API or protocol. Load when S-01 is or may be mode=integration — webhooks, OAuth, third-party APIs, trackers, chat, MCP or any boundary the factory does not own.
---
# Mode: integration

**Signals:** a product or protocol name (GitHub, Linear, Slack, OAuth, MCP …), "connect", "sync", "webhook", "import from"; behaviour depends on a system outside this repository.

**Gather first:**
- The external system's official documentation for the parts used: endpoints, events, auth, versions. Cite the doc page.
- How this repository already talks to it or to similar systems, and the client code it would reuse.
- Earlier issues about limits, outages or surprises with that system.

**Open points worth asking:**
- Which account, tenant or environment it runs against, and who owns its credentials (by reference, never the value).
- Which direction data flows and which side is the source of truth.
- What should happen when the other side is slow, down or changes its contract.

**The spec must make clear:**
- The protocol contract: messages or calls used, required fields, auth, versioning.
- Limits: rate limits, sizes, quotas, timeouts, pagination.
- Failure modes: retries, duplicates, partial success, what the owner sees.
- What can only be verified live against the real system, and what is proven with fakes or recorded fixtures.

Judgment, not a template: skip what does not apply and say why in one line.
