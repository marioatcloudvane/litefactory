# US-005 unit tests

Frozen #270 revision `05c0a9d83aa275880740f7da56895aec235b2c7736f625955c152c12735bc9ad`.
Applied unit-test role after production under explicit parent authorization due
unavailable agent thread capacity. Tests use deterministic supplied time and no sleeps.

`internal/agentresult/progress_test.go` covers:
- AC-001: attribution, positive sequences, exact replay, conflicting replay, unseen
  older status, sequence gaps, latest coalescing, revocation and fresh authority reset.
- AC-002: ASCII and multibyte exact 2,048-byte acceptance/2,049-byte rejection,
  malformed UTF-8/blank messages, 32 identities and first eviction, retained versus
  evicted conflicts, all 1,024 accepted updates and first refused update, replay
  after budget exhaustion, and copied views/baselines.
- AC-003: terminal 24-hour boundary to a nanosecond, earlier explicit cleanup,
  repeated marking cannot extend expiry, active state does not expire as terminal,
  expired reducer calls clear text, and terminal-time copy isolation. Advisory text
  has no authority-bearing API or result-store capability.

Executed `go test -race -cover ./internal/agentresult`: PASS, package coverage 90.5%.
Output: `US-005-unit-tests.log`.
Executed pinned golangci-lint v2.13.1 against `./internal/agentresult`: PASS, 0 issues.
Output: `US-005-lint.log`.
Production/test files were gofmt-formatted. No integration environment is required
for this pure reducer. Full repository aggregate checks remain the final story gate.
