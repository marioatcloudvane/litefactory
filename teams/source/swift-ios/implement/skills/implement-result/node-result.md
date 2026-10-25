Retained review content is separate from an accepted application result. A bounded,
authorized rejected submission may be saved and delivered with its diagnostics and
unaccepted status; it does not satisfy this result contract, advance a workflow or
change any owner gate. Correct the submission and use a new request identity. Exact
replay must keep the original request identity and bytes, then recover its receipt.
Keep the complete authored report or plan within the input ceiling; never shorten a
plan merely to pass validation. Full retained owner output uses ordered numbered
parts linked from the short report. Delivery remains pending until every part has a
confirmed receipt. Shared contributions remain bounded at 4 MiB; Plan node results
remain bounded at 1 MiB and the legacy Plan proposal at 8 MiB. Retention and remote
delivery can fail separately; do not claim missing content was retained or delivered.

Deliver your result only through the shared result route: communication
preflight, submit and receipt. Do not print a JSON result; terminal text is never
read as a result. Each worked result below is exactly what you pass to preflight
and submit. Replace the illustrative content, keep the shape.

# completed

The story is built and its unit tests pass. No questions and no reason.

When you decided a gap the plan did not foresee, list each decision in
`deviations` as one short line: what you chose, why, and file:line. Leave the
list out when there is none, and never mark a deviation in the code.

```json
{
  "result": {
    "outcome": "completed",
    "comment": "Summarise what was built and which tests you ran, in a sentence or two, and name each deviation in plain words; do not submit this example unchanged.",
    "payload": {
      "implement": {
        "stories": [],
        "deviations": [
          "Chose best-effort delivery with a retry key, because the provider removes duplicates only for a few minutes (path/to/file:42)."
        ]
      }
    }
  }
}
```

# incomplete

Part of the story is not done, and your time is about to run out or something
outside the story stops the work for now. Unfinished tasks alone are not a reason
to stop. Say what is left in plain language. The factory runs another attempt
with your reason, and parks the story and asks the owner only once the attempts
are spent.

```json
{
  "result": {
    "outcome": "incomplete",
    "comment": "Say what is built and what is not.",
    "reason": "Say plainly what is left to do and what stopped the work.",
    "payload": {
      "implement": {
        "stories": []
      }
    }
  }
}
```

# blocked

Every option would contradict an approved decision or a frozen acceptance
criterion, or the work needs access or credentials you cannot get. A gap you can
decide within the story's intent is a deviation on a completed result, not a
blocker. Say what blocks you. The story is parked at once and the owner is asked.

```json
{
  "result": {
    "outcome": "blocked",
    "comment": "Say what is built before the blocker.",
    "reason": "Say plainly which approved decision or criterion every option would break, or which access you lack.",
    "payload": {
      "implement": {
        "stories": []
      }
    }
  }
}
```

Only the sync resolver may add `questions` to its `blocked` (or `incomplete`)
result; every other Implement agent's result with questions is refused. Each
question has an `id`, an `audience`, its `text`, 2 to 3 `options` and the
`recommended_option`:

```json
{
  "result": {
    "outcome": "blocked",
    "comment": "Say which conflicts are resolved and which one is not.",
    "reason": "Say plainly which file still conflicts and why it needs a decision.",
    "questions": [
      {
        "id": "q1",
        "audience": "technical",
        "text": "Ask the one decision the conflict needs.",
        "options": [
          {"id": "a", "text": "Keep the feature branch's version."},
          {"id": "b", "text": "Take the base's version."},
          {"id": "c", "text": "Combine both as described here."}
        ],
        "recommended_option": "c"
      }
    ],
    "payload": {
      "implement": {
        "stories": []
      }
    }
  }
}
```
