---
name: research-mode-bug
description: Research mode for a bug or regression that arrived in Research instead of the Bug workflow. Load when S-01 is or may be mode=bug — something used to work or is documented to work and does not.
---
# Mode: bug

**Signals:** "broken", "fails", "regression", "since version …", an error message or log; documented or earlier behaviour differs from what happens now.

**Gather first:**
- The reproduction: steps, input, logs, run IDs the owner gave. Try it with read tools where you can.
- Recent commits and merged PRs in the affected area — a regression usually has a cause there.
- Existing Bug issues for the same symptom, open or closed.

**Open points worth asking:**
- A reproduction or log you cannot obtain yourself.
- Which behaviour is expected when docs, code and the owner disagree.
- Whether a quick containment matters more than the full fix.

**The spec must make clear:**
- Current behaviour, expected behaviour and what must stay unchanged — each with evidence (permalink, log line, test). Mark what is observed versus inferred.
- The suspected cause, or the remaining hypotheses if unproven.
- How the fix will be shown to work: the reproduction turned into a check.

If the work is small and bounded, say so: the Bug workflow may suit it better. That is a recommendation, not a mode switch.
