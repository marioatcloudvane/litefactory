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
