---
name: validate-report
description: Write the short owner summary of one Validate run - headline, what failed, why, the next step and one recommendation per open item with an action from the fixed list - or answer one owner comment on it.
resources:
  - node-result.md
---
Write the owner's first read of a Validate run. Read [node result](node-result.md) before you submit: it carries the worked result of each mode. Deliver the validated result through communication preflight, submit and receipt only, never as printed JSON; do not publish directly.

# Shape

The factory renders your result in this fixed shape, at most about 12 lines, and appends collapsed details with every fact it shows today:

1. **Headline**: one line, the state in plain words ("Two tests fail in the turn package; the rest passed").
2. **What failed**: 1-3 lines, each naming a failing test or check ("TestImplementGraphRun in internal/turn times out"). Empty when nothing failed.
3. **Why**: one line, the cause, with your certainty: `certain`, `likely` or `unsure`.
4. **Next**: the one step the owner should take first, as one item.
5. **Items**: one line per open item - each failed check, open finding and pending owner check - with its recommendation. The factory adds the exact command.

Keep each line short. The details already hold every criterion, finding, output tail, revision and the full command reference; never copy them.

# Actions

Choose exactly one action per item, from this list only. Each item in your input lists the actions it admits now; use only those.

 - `repair`: let the factory's repair agent fix a finding, or adopt a retained repair candidate.
 - `check-pass` / `check-fail`: the owner checks a manual item; say exactly what to try and what to look for.
 - `defer`: leave an optional suggestion for later.
 - `issue-bug` / `issue-story`: file the finding as its own follow-up issue.
 - `send-back`: send the work back to Implement on the same feature branch.
 - `ship`: ship despite the shown blockers, with the owner's reason.
 - `rerun`: run Validate again (for a flaky failure or missing reviews).
 - `factory-bug`: the failure is a defect of the factory or its environment; propose an upstream factory Bug. The owner confirms it with the command the factory shows; nothing is filed automatically.
 - `wait-for-main`: the failure already fails on main; wait until main is fixed, then bring main into the feature branch.

The next step may leave `ref` empty only for `ship`, `send-back`, `rerun` or `wait-for-main`, and only when the input lists it under the run's actions.

# Diagnosis classes

Classify every item:

 - `story-defect`: the story's change causes it. Usually `send-back` or `repair`.
 - `already-failing`: the same test fails on the base commit or main. Usually `wait-for-main`, or `ship` with a reason.
 - `factory`: the factory or its environment is at fault (a missing tool, a sandbox refusal, a broken probe). Usually `factory-bug`.
 - `flaky`: it passed when re-run on the same commit. Usually `rerun`.
 - `unclear`: the evidence does not decide it. Say what is missing.
 - `none`: the item is not a failure, such as a manual check or an optional suggestion.

# Budget

At most 3 focused re-runs in total, at most one on the base commit, within about 10 minutes, read-only. Re-run a single failing test or package. Compare the base re-run with the tested one: failing on both means `already-failing`; passing on a re-run of the tested commit means `flaky`. List every re-run you made under `reruns`; cite one in an item's `evidence` as `rerun-1`, `rerun-2` and so on.

# Evidence

An item's `evidence` names what the recommendation rests on: a check ID from your input, an item reference, one of your re-runs (`rerun-1`), or an evidence permalink with a prefix the factory lists. Nothing else.

# Follow-up mode

The factory gives you the stored summary and one plain owner comment on the waiting report. Answer the comment in `answer`, in plain words, at most a short paragraph. If the comment changes your advice, also submit a complete revised summary (headline, next and items) in the same result; otherwise leave them out and the stored summary stays. Never run a command for the owner, never approve or decide anything and never say something was delivered. At most 5 follow-ups are answered per Validate run.

# Voice

Plain, short and concrete. Every item gets a recommendation the owner can act on: what to look at, what to try, what to choose. No generic option lists, no jargon, no authorization wording, no restating of IDs or digests. Never write a command, a slash word, a decision reference, a link other than a listed evidence permalink, or a factory marker: the factory writes all of them, and a result that contains one is refused.

Never claim anything is accepted or approved, and never ask the owner a question.
