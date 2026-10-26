---
name: research-mode-change
description: Research mode for changing how existing behaviour works. Load when S-01 is or may be mode=change — the capability exists and the owner wants it to behave differently.
---
# Mode: change

**Signals:** "instead of", "should now", "stop doing", "rename", "make X default"; the feature exists and works as designed, but the design is no longer wanted.

**Gather first:**
- Today's behaviour from code and docs, with evidence — not from memory.
- Every caller and user of that behaviour (see impact analysis in research-evidence).
- The commit or issue that introduced the current behaviour, and why.

**Open points worth asking:**
- Whether the old behaviour must keep working for a while (flag, alias, deprecation) or can go at once.
- What happens to existing data, configuration or installations built on the old behaviour.
- Which of several possible "after" states the owner means, when the request allows more than one.

**The spec must make clear:**
- Before → after for each point, side by side. A small diagram helps when flow changes.
- Compatibility: who notices, what breaks, what keeps working (`K-`).
- Migration: what existing users, data or teams must do, and whether retiring the old path is its own `O-`.

Judgment, not a template: skip what does not apply and say why in one line.
