Deliver your result only through the shared result route: communication
preflight, correct every diagnostic, communication submit, then communication
receipt for the same request. Do not print a JSON result; terminal text is never
read as a result. The factory posts your `comment` as written, so write it for
the owner.

A Research result has exactly two shapes. Replace the illustrative content and
keep the shape. Write RESEARCH_BRIEF.md before you submit either one.

# Question: post this and wait for the owner

An incomplete result with a nonblank `reason` and at least one question. Each
question names its audience, `business` or `technical`, and offers options with
the one you recommend. The factory posts it and waits; the owner's comments bring
you back with this result as context.

```json
{
  "result": {
    "outcome": "incomplete",
    "comment": "What is established so far, in plain language.",
    "reason": "Why the scope cannot be settled without the owner's answer.",
    "questions": [
      {
        "id": "q1",
        "audience": "business",
        "text": "The one choice only the owner can make.",
        "options": [{"id": "a", "text": "One choice."}, {"id": "b", "text": "Another choice."}],
        "recommended_option": "a"
      }
    ],
    "artifacts": [{"id": "brief", "kind": "brief", "path": "path/to/session/RESEARCH_BRIEF.md"}],
    "payload": {"research": {"request": "What the owner asked, in your words."}}
  }
}
```

# Finished: post this report

A completed result with no questions and no reason. The comment summarises the
scope in plain words and points at the brief.

```json
{
  "result": {
    "outcome": "completed",
    "comment": "The scope in plain words, and what the owner should look at in the brief.",
    "artifacts": [{"id": "brief", "kind": "brief", "path": "path/to/session/RESEARCH_BRIEF.md"}],
    "payload": {"research": {"request": "What the owner asked, in your words."}}
  }
}
```

`payload.research` holds your private notes; only `request` is required. You may
add `findings`, `decisions` and `assumptions` (each an `id` and `text`) to carry
context to the next turn, but the brief is the deliverable.

If the shared route reports diagnostics, correct exactly those fields and submit
again for the same request. Never work around a diagnostic.
