---
name: research-mode-feature
description: Research mode for a new capability that does not exist yet. Load when S-01 is or may be mode=feature — the request adds something users or systems could not do before.
---
# Mode: feature

**Signals:** "add", "new", "support for", "let users …"; no current code path does it; the owner describes an outcome, not a defect.

**Gather first:**
- The closest existing capability and how it is built, so the new one fits beside it.
- Open and closed issues/PRs asking for the same or something adjacent.
- Who will use it (owner, CLI user, agent, external client) and through which surface.

**Open points worth asking (only what context cannot answer):**
- Who it is for and the one situation it must handle well.
- The smallest version that is still worth having; what can wait.
- Where it lives: new command, option, mode, page — when more than one fits.

**The spec must make clear:**
- The primary user story and the outcome as `O-` entries.
- What is deliberately left out (`N-`) and what existing behaviour must not move (`K-`).
- Data model, interface surface and permissions — only those the feature actually touches.
- Edge cases and how a user tells it works.

Judgment, not a template: skip what does not apply and say why in one line.
