---
name: spike-result
description: Submit a Spike result in one of its two shapes, a question for the owner or a finished report.
---
Deliver your result only through the shared result route: communication preflight, correct every diagnostic, communication submit, then communication receipt for the same request. Do not print a JSON result; terminal text is never read as a result. The factory posts your `comment` as written, so write it for the owner.

A Spike result has exactly two shapes. Replace the illustrative content and keep the shape.

# Question: post this and wait for the owner

An incomplete result with a nonblank `reason` and at least one question. Each question names its audience, `business` or `technical`, and should offer options with the one you recommend. The factory posts it and waits; the owner's reply brings you back with this result as context.

```json
{
  "result": {
    "outcome": "incomplete",
    "comment": "What you established so far, in plain language.",
    "reason": "Why you need the owner's answer before going on.",
    "questions": [
      {
        "id": "q1",
        "audience": "technical",
        "text": "The one choice only the owner can make.",
        "options": [{"id": "a", "text": "One choice."}, {"id": "b", "text": "Another choice."}],
        "recommended_option": "a"
      }
    ],
    "payload": {
      "spike": {
        "request": "What the owner asked, in your words.",
        "work_done": ["What you did."],
        "findings": ["What you found."]
      }
    }
  }
}
```

# Finished: post this report, the Spike is done

A completed result with no questions and no reason. The comment is the answer or what changed, what you checked and the result, and anything left open.

```json
{
  "result": {
    "outcome": "completed",
    "comment": "The answer or the change, the checks you ran and their results, and anything left open.",
    "payload": {
      "spike": {
        "request": "What the owner asked, in your words.",
        "work_done": ["What you did, including each check you ran and its result."],
        "findings": ["What you found."],
        "decisions": ["A decision worth remembering."],
        "not_done": ["Anything left open, and why."]
      }
    }
  }
}
```

The `payload.spike` lists are your private notes; only `request` is required. They carry your context to the next turn.

If the shared route reports diagnostics, correct exactly those fields and submit again for the same request. Never work around a diagnostic.
