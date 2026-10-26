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
read as a result. The accepted submission is this step's result, and the factory
derives the step's response from it. Each worked result below is exactly what you
pass to preflight and submit. Replace the illustrative content, keep the shape.

# response done, no change proposed

A completed result with no questions and no reason. Record the request, what you
did, what you found, what you decided and what you did not do. Omit the proposed
fix when no change is warranted.

```json
{
  "result": {
    "outcome": "completed",
    "comment": "Summarise the answer in a sentence or two; do not submit this example unchanged.",
    "payload": {
      "spike": {
        "request": "Restate the question that was asked.",
        "work_done": ["Describe one thing you did."],
        "findings": ["Record one observed fact."],
        "decisions": ["Record one decision worth remembering."],
        "not_done": []
      }
    }
  }
}
```

# response done, one small change proposed

Add the proposed fix when exactly one defect explains the question and one small
change with a regression test would fix it. List every file you would change,
including the regression test file, and every kind of change.

```json
{
  "result": {
    "outcome": "completed",
    "comment": "Root cause found; one small change is proposed.",
    "payload": {
      "spike": {
        "request": "Restate the question that was asked.",
        "work_done": ["Describe one thing you did."],
        "findings": ["Record the root cause."],
        "proposed_fix": {
          "defects": ["Name the one defect."],
          "files": ["path/to/{{example_file}}", "path/to/{{example_test_file}}"],
          "change_kinds": ["code", "test"],
          "regression_test": "Name of the test that fails before and passes after."
        }
      }
    }
  }
}
```

# response done, work is larger

Say so plainly, record what you learned, list what was not done and recommend the
next mode in the recommendation. Do not propose a change.

```json
{
  "result": {
    "outcome": "completed",
    "comment": "This exceeds a Spike; the findings are recorded.",
    "recommendation": {"text": "Take this to Research: it spans several packages."},
    "payload": {
      "spike": {
        "request": "Restate the question that was asked.",
        "findings": ["Record what you learned."],
        "not_done": ["Describe what was left undone and why."]
      }
    }
  }
}
```

# response done, the fix was made (fixer)

A step that was given write access reports the one change it made. A completed
result from such a step must carry a fix report: the one defect, every file you
changed including the regression test file, the regression test file (it must be
among the files), the risky kinds of change you touched (only `package`,
`dependency`, `schema` or `api`; omit the list when you touched none) and a short
summary. Put every blocker or unfinished item under not done. Never claim the
change is checked: the factory checks it after you finish.

```json
{
  "result": {
    "outcome": "completed",
    "comment": "The fix and its regression test are written.",
    "payload": {
      "spike": {
        "request": "Restate the question that was asked.",
        "work_done": ["Describe one thing you did."],
        "not_done": [],
        "fix_report": {
          "defect": "Name the one defect you fixed.",
          "files": ["path/to/{{example_file}}", "path/to/{{example_test_file}}"],
          "regression_test": "path/to/{{example_test_file}}",
          "change_kinds": [],
          "summary": "One or two sentences on what changed and why."
        }
      }
    }
  }
}
```

# response done, the fix was made (fixer)

A step that was given write access reports the one change it made. A completed
result from such a step must carry a fix report: the one defect, every file you
changed including the regression test file, the regression test file (it must be
among the files), the risky kinds of change you touched (only `package`,
`dependency`, `schema` or `api`; omit the list when you touched none) and a short
summary. Put every blocker or unfinished item under not done. Never claim the
change is checked: the factory checks it after you finish.

```json
{
  "result": {
    "outcome": "completed",
    "comment": "The fix and its regression test are written.",
    "payload": {
      "spike": {
        "request": "Restate the question that was asked.",
        "work_done": ["Describe one thing you did."],
        "not_done": [],
        "fix_report": {
          "defect": "Name the one defect you fixed.",
          "files": ["path/to/{{example_file}}", "path/to/{{example_test_file}}"],
          "regression_test": "path/to/{{example_test_file}}",
          "change_kinds": [],
          "summary": "One or two sentences on what changed and why."
        }
      }
    }
  }
}
```

# response ask

An incomplete result, a non-blank reason and at least one question, once only.
Give every question concrete options and name the one you recommend. Every
question names its audience: `business` or `technical`.

```json
{
  "result": {
    "outcome": "incomplete",
    "comment": "Summarise what is established so far.",
    "reason": "State why the question cannot be answered without the owner.",
    "questions": [
      {
        "id": "q1",
        "audience": "technical",
        "text": "Ask the one question that blocks the answer.",
        "options": [{"id": "a", "text": "Describe one permitted choice."}],
        "recommended_option": "a"
      }
    ],
    "payload": {
      "spike": {
        "request": "Restate the question that was asked.",
        "work_done": ["Describe what you did so far."]
      }
    }
  }
}
```

# Corrections

If the shared route reports diagnostics, correct exactly those fields and submit
again for the same request. Never work around a diagnostic.
