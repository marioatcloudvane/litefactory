---
name: doctor-result
description: Submit a Doctor result in one of its two shapes, a question for the owner or a finished report.
requires: [run-rules, result-protocol]
---
Deliver your result only through the shared result route: communication preflight, correct every diagnostic, communication submit, then communication receipt for the same request. Do not print a JSON result; terminal text is never read as a result. The factory posts your `comment` as written, so write it for the owner.

A Doctor result has exactly two shapes. Replace the illustrative content and keep the shape.

- You need an answer, or you want to repair something and do not yet hold an explicit go for that exact repair: submit a **question**. The comment says what you found and the exact repair you propose; the question asks for the go, with the options to go ahead, change the proposal, or stop.
- You explained the cause without changing anything, or you made the repair a go covered and checked it: submit a **finished** report.

# Question: post this and wait for the owner

An incomplete result with a nonblank `reason` and at least one question. Each question names its audience, `business` or `technical`, and should offer options with the one you recommend.

```json
{
  "result": {
    "outcome": "incomplete",
    "comment": "Need your go: one plain line on the repair. Then the cause and the evidence, in plain language.",
    "reason": "Why you need the owner's answer before going on.",
    "questions": [
      {
        "id": "q1",
        "audience": "technical",
        "text": "The one choice only the owner can make.",
        "options": [{"id": "a", "text": "Go ahead with the repair."}, {"id": "b", "text": "Stop here."}],
        "recommended_option": "a"
      }
    ],
    "payload": {
      "doctor": {
        "request": "What the owner asked, in your words.",
        "findings": ["What you found, with the evidence."]
      }
    }
  }
}
```

# Finished: post this report, the Doctor is done

A completed result with no questions and no reason. After a repair, say that the change is saved on the task branch and not merged yet; the factory publishes its own shipping review and waits for the owner's `ship`.

```json
{
  "result": {
    "outcome": "completed",
    "comment": "One plain sentence on what was wrong or what you repaired. Then the cause, the evidence, each check you ran and its result, and the owner's next step.",
    "payload": {
      "doctor": {
        "request": "What the owner asked, in your words.",
        "work_done": ["What you did, including each check command you ran and its result."],
        "findings": ["What you found."],
        "decisions": ["A decision worth remembering."],
        "not_done": ["What you could not check or change, and why."]
      }
    }
  }
}
```

The `payload.doctor` lists are your private notes; only `request` is required. They carry your context to the next turn.

If the shared route reports diagnostics, correct exactly those fields and submit again for the same request. Never work around a diagnostic.
