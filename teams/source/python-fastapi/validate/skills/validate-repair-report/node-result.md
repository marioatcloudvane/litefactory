Deliver your result only through the shared result route: communication
preflight, submit and receipt. Do not print a JSON result; terminal text is never
read as a result. Each worked result below is exactly what you pass to preflight
and submit. Replace the illustrative content, keep the shape.

The `command` of a failure is the failing suite command exactly as the factory
listed it. `cause` is your understanding of why it failed, or `unknown`.

# repaired

You made a small change for every failure. Name the files you changed for each
failure and what you fixed. The factory measures the change and tests it again.

```json
{
  "result": {
    "outcome": "completed",
    "comment": "Fixed the rounding error that made the totals test fail; the failing test now passes. The factory still needs to measure the change and run its checks.",
    "payload": {
      "validate": {
        "repair": {
          "response": "repaired",
          "failures": [
            {
              "command": "uv run --no-sync pytest tests",
              "observed": "TestTotal expected 10.00 and got 9.99",
              "cause": "The total is rounded down instead of to the nearest cent.",
              "locations": [{"path": "internal/total/total.go", "line": 42}],
              "files_changed": ["internal/total/total.go"],
              "fixed": "Round to the nearest cent."
            }
          ]
        }
      }
    }
  }
}
```

# handback

The fix is large, or you are not sure of the cause. Change nothing. For each
failure give the expected behavior and one exact objective for Implement. When
the cause is unknown, use `unknown` and make the objective a concrete diagnostic
step.

```json
{
  "result": {
    "outcome": "completed",
    "comment": "Two failures need more than a small fix; nothing was changed.",
    "payload": {
      "validate": {
        "repair": {
          "response": "handback",
          "failures": [
            {
              "command": "uv run --no-sync pytest tests",
              "observed": "TestSync times out after 60 seconds",
              "cause": "unknown",
              "locations": [{"path": "internal/sync/sync.go"}],
              "expected": "Sync returns within a second for an empty queue.",
              "objective": "Add a log line before and after each lock in Sync, run TestSync once, and find which lock is never released."
            }
          ]
        }
      }
    }
  }
}
```

# blocked

You cannot work at all, for example the failing output is missing. Say why.

```json
{
  "result": {
    "outcome": "blocked",
    "comment": "I could not start.",
    "reason": "The failing output was empty, so there is nothing to repair.",
    "payload": {
      "validate": {
        "repair": {
          "response": "blocked",
          "reason": "The failing output was empty, so there is nothing to repair."
        }
      }
    }
  }
}
```
