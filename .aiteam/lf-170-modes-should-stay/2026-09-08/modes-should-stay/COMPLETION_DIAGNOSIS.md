# Completion diagnosis — run 0004

The factory-supplied run 267 bundle marks all four selected stories blocked. The prior assistant's statement that US-001–003 were verified described locally produced evidence, not confirmed factory acceptance. Local source and the retained run-257 event helper reveal a deterministic acceptance mismatch affecting every supplied frozen body.

## Confirmed acceptance mismatch

Every supplied frozen body puts the 346-byte paragraph beginning `Evidence contract: publish the factory-supplied per-run story evidence` inside `## Acceptance Criteria`, after the AC checklist and before `## Tasks`.

`internal/planbrief/acceptance.go:34` keeps the acceptance section active until the next level-2 heading. Lines 44–45 skip only empty/out-of-section lines. Lines 70–83 name every remaining line by its AC identifier or the entire line. Therefore the paragraph is an additional required criterion, beyond the four or six numbered AC identifiers.

`internal/turn/story_progress.go:508` calls this parser and requires every resulting name to exist in acceptance evidence; an AC-only event fails with `missing scoped evidence for Evidence contract: ...`. The retained `/private/tmp/lf170_event.py` specifically asserts the evidence keys are exactly the numbered AC identifiers, so the helper's verified events cannot satisfy these bodies under this source.

Adding that paragraph as another check cannot repair the mismatch: it is 346 UTF-8 bytes, while `internal/agent/story_progress.go:149` rejects check names longer than 256. This is an unsatisfiable frozen acceptance contract, not a missing product implementation or test failure. No source code was changed during this diagnosis.

## Why a local verified event is insufficient

`internal/turn/story_progress.go:278` parses each event, checks frozen identity and authority, checks acceptance coverage, and compares actual worktree bytes to its snapshots before accepting it. Lines 333–338 persist a rejected event on failure without giving its asserted story identity authority. `internal/store/story_progress.go:282` persists such rejection with empty story key and attempt zero.

Only accepted verification changes the managed story to `verified` (`internal/store/story_progress.go:159`). The factory then posts evidence and closes the numerically verified child (`internal/turn/story_progress.go:445` and `:472`). Only successful effect acknowledgement sets the managed story to `completed` (`internal/store/story_progress.go:210`). Producing a JSON file and reporting success cannot substitute for these steps.

The helper's manifest construction, base64 content, identities and names follow the supplied event schema. Its sequence is based on existing immutable files and its verification attempt increments for rework. This reveals no independent initial-envelope defect. Rejection means the factory's accepted per-story attempt history can diverge from the helper's emitted history; future runs must restart attempts at 1 and use fresh run bindings.

Parent termination does not indiscriminately undo completed siblings: `EndStoryProgress` at `internal/store/story_progress.go:232` emits blocked notices only for stories outside `completed` and `verified`. Thus US-004's documentation blocker alone does not explain US-001–003 being blocked. An accepted invalidation sets the story blocked; a subsequent rejected verification leaves it blocked. This is consistent with the supplied current state but the exact prior event sequence and rejection reason were not recovered.

## What “ticked off” means in the current implementation

`internal/github/story_state.go:8` explicitly changes issue state only and preserves story content; its update at line 39 contains only state and state reason. Accepted verification closes a child issue and posts its evidence. It does not change `[ ]` acceptance checkboxes to `[x]` or change task-table `TODO` cells. Those Markdown fields can therefore stay unchecked even after an actual successful closure. Immutable approved revisions must not be changed by an agent to simulate completion.

## Next steps and limits

1. Resolve the incompatible acceptance-section prose through a factory-owned approved story revision (move transport instructions to a separate level-2 section), or a deliberately reviewed parser/protocol change. Do not rewrite the selected revision, spoof its digest, or publish impossible verified evidence.
2. After a valid frozen contract is supplied, verify the retained implementation and emit current-run stable snapshots with every required acceptance name; distinguish submitted evidence from factory-confirmed completion in final reports.
3. Keep the separate US-004 Codex skill write-permission blocker visible until source/copy parity can actually be completed.

This diagnosis reads repository source and the retained helper only. It does not fetch or mutate GitHub, read daemon runtime databases/logs, inspect credentials, or claim the actual run-257 daemon rejection text. The exact historical rejection requires factory-owned diagnostics. No production files or tests were modified or run by this diagnostic subtask.

## Operational confirmation

The separate read-only receipt investigation confirmed the actual run-257 rejections, superseding the source-only investigation's lack of historical receipts. `~/.litefactory/logs/daemon.log:94664` (2026-09-09 14:49:17 +02:00) rejects event 1 for missing scoped evidence for the Evidence contract paragraph. The same rejection appears for events 3/4, 5, 10/11 and 12 at lines 94809, 94967, 95090 and 95134. Lines 95393–95397 record the successful parent run/save and commit d3ab082715819ef0635e4469c452c3fc405787ff. A saved run was therefore not accepted per-story completion.

Run 267 publishes four blocked events with this concrete cause. No production code, frozen issue bodies, live forge state or runtime state was changed. The owner asked for an explanation; no unrelated parser or checklist feature was implemented. Previously passing tests were not rerun for this read-only investigation.
