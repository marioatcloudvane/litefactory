---
name: bug-result
description: Submit a Bug result in one of its two shapes, a question for the owner or a finished report.
requires: [run-rules, result-protocol]
---
Deliver your result only through the shared result route: communication preflight, correct every diagnostic, communication submit, then communication receipt for the same request. Do not print a JSON result; terminal text is never read as a result. The factory posts your `comment` as written, so write it for the owner.

A Bug result has exactly two shapes. Replace the illustrative content and keep the shape.

# Question: post this and wait for the owner

An incomplete result with a nonblank `reason` and at least one question. Each question names its audience, `business` or `technical`, and offers options with the one you recommend.

```json
{
  "result": {
    "outcome": "incomplete",
    "comment": "Need your answer: one plain line. Then what you found, in plain language.",
    "reason": "Why the owner must decide before you go on.",
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
      "bug": {
        "request": "The reported bug, in your words.",
        "work_done": ["The failing test you wrote and what it showed."],
        "findings": ["The cause, or what is still unclear."]
      }
    }
  }
}
```

# Finished: post this report

A completed result with no questions and no reason. With a fix, the factory saves it, opens the PR and publishes a shipping review; without a change, it posts your report and the Bug is done.

```json
{
  "result": {
    "outcome": "completed",
    "comment": "Fixed: one plain sentence. Then the cause, the change, the failing run before the fix and the passing run after it, what you could not run, that the fix is saved on the task branch and not merged yet, and anything left open.",
    "payload": {
      "bug": {
        "request": "The reported bug, in your words.",
        "work_done": ["`<test command>` failed before the fix: <what it showed>.", "`<test command>` passed after the fix."],
        "findings": ["The cause and where it was."],
        "decisions": ["A decision worth remembering."],
        "not_done": ["Each check you could not run and why."]
      }
    }
  }
}
```

The `payload.bug` lists are your notes; only `request` is required. They carry your context to the next turn. If the shared route reports diagnostics, correct exactly those fields and submit again for the same request.
