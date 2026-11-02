Deliver your complete result only through the shared result route: communication
preflight, correct every diagnostic, submit, then read the receipt for the same
request_id. The shared result helper's preflight, submit and receipt methods use
the same route. The accepted submission is this step's result. Do not print a JSON
result: terminal text is never read as a result, so end with a short plain-language
note. Read the worked results below before submitting; replace illustrative content
and keep the shape.

Retained review content is separate from an accepted application result. A bounded,
authorized rejected submission may be saved and delivered with its diagnostics and
unaccepted status; it does not satisfy this result contract, advance a workflow or
change any owner gate. Correct the submission and use a new request identity. Exact
replay must keep the original request identity and bytes, then recover its receipt.

The result is `completed` with `payload.validate.report`. Use only item references
and actions from your input. `ref` is a finding ID (`story-001:AC-001`), a failure
index (`failure-1`), a check ID or a repair candidate ID, exactly as given.

# report

The first summary of a tested commit.

```json
{
  "result": {
    "outcome": "completed",
    "comment": "Summary written: one test fails, and it fails on main too.",
    "payload": {
      "validate": {
        "report": {
          "mode": "report",
          "headline": "One test fails in the turn package; it fails on main too. Two items need your check.",
          "what_failed": ["TestImplementGraphRun in internal/turn: no owner wait is recorded after a sync conflict"],
          "why": "The same test fails on the base commit, so this story did not cause it.",
          "certainty": "likely",
          "next": {"ref": "failure-1", "action": "wait-for-main", "recommendation": "Wait until main is fixed, then bring main into the feature branch.", "diagnosis": "already-failing", "evidence": "rerun-1"},
          "items": [
            {"ref": "story-001:AC-001", "action": "check-pass", "recommendation": "Upload a real invoice and confirm the dialog shows what it became.", "diagnosis": "none"},
            {"ref": "story-001:plan", "action": "defer", "recommendation": "The staged verification is optional; defer it.", "diagnosis": "none", "evidence": "go test ./..."}
          ],
          "reruns": [
            {"command": "go test ./internal/turn -run TestImplementGraphRun", "commit": "base", "result": "failed", "note": "Fails the same way on the base commit."}
          ]
        }
      }
    }
  }
}
```

# follow-up

An answer to one owner comment. Add headline, next and items only when the
comment changes your advice; they then replace the stored summary.

```json
{
  "result": {
    "outcome": "completed",
    "comment": "Answered the owner's question.",
    "payload": {
      "validate": {
        "report": {
          "mode": "follow-up",
          "answer": "Yes, it is safe to wait: the failing test fails the same way on main, and nothing in this story touches that code."
        }
      }
    }
  }
}
```
