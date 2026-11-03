---
name: spike-result
description: Submit a Spike result in one of its two shapes, a question for the owner or a finished report.
---
Deliver your result only through the shared result route: communication preflight, correct every diagnostic, communication submit, then communication receipt for the same request. Do not print a JSON result; terminal text is never read as a result. The factory posts your `comment` as written, so write it for the owner.

A Spike result has exactly two shapes. Replace the illustrative content and keep the shape.

Pick the shape by what you are allowed to do next:

- You need an answer, or you want to change code and do not yet hold an explicit go ("go", "do it", "build it", "yes, implement it") for that exact change: submit a **question**. An answer to an earlier question, new details or a described wish is not a go. To ask for the go, the comment states what you found and the exact change you propose (what, where, why, which checks you will run), and the question asks for the go, with the options to go ahead, change the proposal, or stop.
- You answered without changing code, or you made the change a go covered and checked it: submit a **finished** report.
- A go turned out to cover less than the work needs: stop editing and submit a **question** that says what you already changed and asks for a new go.

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

A completed result with no questions and no reason. The comment is the answer or what changed, each check you ran and its result, and anything left open.

After a change, run the repository's own checks for the files you changed before you submit: its build, its focused tests and its linter or formatter check, using the commands the project itself uses (agent guidance files, Makefile, task runner, CI configuration, package scripts). Name each command and its result. Say plainly what you could not run and why, and what that leaves unchecked. The factory saves the change on the task branch; do not say it is merged or delivered.

```json
{
  "result": {
    "outcome": "completed",
    "comment": "The answer or the change, each check you ran and its result, what you could not run, and anything left open.",
    "payload": {
      "spike": {
        "request": "What the owner asked, in your words.",
        "work_done": ["What you did, including each check command you ran and its result."],
        "findings": ["What you found."],
        "decisions": ["A decision worth remembering."],
        "not_done": ["Each check you could not run and why, and anything else left open."]
      }
    }
  }
}
```

The `payload.spike` lists are your private notes; only `request` is required. They carry your context to the next turn.

If the shared route reports diagnostics, correct exactly those fields and submit again for the same request. Never work around a diagnostic.
