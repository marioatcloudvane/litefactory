---
name: research-mode-refactor
description: Research mode for restructuring code without changing behaviour. Load when S-01 is or may be mode=refactor — split, merge, move, rename internals, remove duplication or dead code.
---
# Mode: refactor

**Signals:** "clean up", "split", "extract", "unify", "remove duplication", "dead code"; the owner expects users to notice nothing.

**Gather first:**
- The current structure and its dependents (see impact analysis in research-evidence).
- The tests that cover the area today, and where coverage is thin.
- Earlier refactors or reverts in the same area, and why they happened.

**Open points worth asking:**
- Where the new boundaries should sit, when more than one cut is reasonable.
- Whether it may be done in steps that each ship, or must land at once.
- Whether a small behaviour change found on the way is allowed or must become its own work.

**The spec must make clear:**
- Invariants as `K-` entries: the observable behaviour, data and interfaces that must not change.
- The safety net: which existing checks prove the invariants, and what check must be added first where none exists.
- The target structure in a sentence or a small diagram, and what gets deleted.
- What is superseded and must be retired (its own `O-`), so old and new do not live side by side.

Judgment, not a template: skip what does not apply and say why in one line.
