---
name: environment-validator
description: Documents the pre-tool hook that enforces test environment safety before integration tests are allowed to run. Contains the hook script template, settings.json configuration, and setup instructions. Referenced by the implement-orchestrator during Phase 2.
---

# Environment Validator — Test Safety Hook

A Claude Code pre-tool hook that runs before every Bash command and blocks integration test commands if the environment is not correctly configured. It cannot be bypassed by the orchestrator or any sub-agent — it enforces safety at the tool layer, not the prompt layer.

This is the second enforcement layer. The first is the orchestrator's Phase 2 conversation. Both are needed: the conversation handles setup coordination; the hook prevents accidental mis-fires.

---

## What it checks

For any Bash command matching `go test` with an integration marker (`-tags=integration`, `-tags integration`, or `-run Integration`):

| Check | Env var | Triggered by |
|---|---|---|
| URL is set | `TEST_BASE_URL` | Any integration test command |
| URL is not production | `TEST_BASE_URL` | Any integration test command |
| Test database is configured | `TEST_DATABASE_URL` | Integration test commands |

There is no UI test tier in this lightweight team, so no `TEST_USER`/`TEST_PASSWORD` checks are needed.

---

## Hook configuration

Add to your project's `.claude/settings.json`:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": ".claude/hooks/check-test-env.sh"
          }
        ]
      }
    ]
  }
}
```

---

## Hook script

Create `.claude/hooks/check-test-env.sh` in your project and make it executable (`chmod +x`):

```bash
#!/usr/bin/env bash
set -euo pipefail

# Read the Bash command Claude is about to run
INPUT=$(cat)
CMD=$(echo "$INPUT" | python3 -c "
import sys, json
d = json.load(sys.stdin)
print(d.get('tool_input', {}).get('command', ''))
" 2>/dev/null || echo "")

# Only intercept integration test commands
if ! echo "$CMD" | grep -qE 'go test' || ! echo "$CMD" | grep -qE '(-tags[= ]integration|-run[= ]?.*Integration)'; then
  exit 0
fi

ERRORS=()

# TEST_BASE_URL must be set and must not be production
if [ -z "${TEST_BASE_URL:-}" ]; then
  ERRORS+=("TEST_BASE_URL is not set. Set it to your test/staging environment URL.")
else
  if echo "$TEST_BASE_URL" | grep -qiE '(//app\.[^/]|//api\.[^/]|\.prod\.|//prod\.|production\.)' && \
     ! echo "$TEST_BASE_URL" | grep -qiE '(staging|test|dev|localhost|127\.0\.0\.1)'; then
    ERRORS+=("TEST_BASE_URL looks like a production URL: $TEST_BASE_URL")
    ERRORS+=("Tests must never run against production.")
  fi
fi

# Integration tests need TEST_DATABASE_URL
if [ -z "${TEST_DATABASE_URL:-}" ]; then
  ERRORS+=("TEST_DATABASE_URL is not set. Integration tests require a test database.")
fi

if [ ${#ERRORS[@]} -gt 0 ]; then
  echo "" >&2
  echo "⛔ TEST ENVIRONMENT NOT CONFIGURED — test run blocked." >&2
  echo "" >&2
  for err in "${ERRORS[@]}"; do
    echo "  • $err" >&2
  done
  echo "" >&2
  echo "Set the required environment variables and retry." >&2
  exit 1
fi

exit 0
```

---

## Environment variables reference

| Variable | Required for | Example value |
|---|---|---|
| `TEST_BASE_URL` | Integration tests | `http://localhost:8080` or `https://staging.yourapp.com` |
| `TEST_DATABASE_URL` | Integration tests | `postgres://postgres:password@localhost:5432/test_myapp?sslmode=disable` |

Note: if integration tests use `testcontainers-go` to spin up their own database, `TEST_DATABASE_URL` may not be needed — the hook's check is aimed at the shared-test-database convention. Adjust the hook if the project standardises on testcontainers instead.

---

## Setting up for a local run

```bash
export TEST_BASE_URL="http://localhost:8080"
export TEST_DATABASE_URL="postgres://postgres:password@localhost:5432/test_myapp?sslmode=disable"
```

For CI, inject as repository secrets. Never commit values to any artifact file.
