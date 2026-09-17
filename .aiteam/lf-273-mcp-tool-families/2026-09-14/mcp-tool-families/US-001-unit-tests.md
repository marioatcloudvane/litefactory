# US-001 unit execution

Frozen story #281, revision c75f0fc994ded9287b03caa2e600cecd911d5d962b043b5f73aa6be4b913a6d0, dependencies none, workflow 2.

Changed test path: `internal/turn/receipts_unit_test.go`.

Five top-level tests, 19 leaf cases passed, zero failed. Tests are pure/in-memory: inert unopened database handles are supplied only to construction; malformed lookup uses a zero service and must reject before observation. No network, filesystem, or database operations occur in these tests.

AC-002: missing binding and foreign repository/task or malformed historical scopes refuse construction; caller mutation cannot alter copied history or registered projection permission names; returned permission bytes cannot mutate the service; private work-context/hash sentinel values are excluded. Removing history or changing historical attempt changes permission material; duplicate identical history does not.

AC-003: missing/mismatched/oversized request IDs, caller-selected scopes, duplicate JSON fields, and trailing JSON reject before observation. Permission inspection does not invoke a domain projection.

AC-001 journal before transmission, cross-family correlation, communication compatibility, lost replies and reopening require filesystem/socket access because Call exposes no pure correlation seam; those cases belong to the assigned integration specialist. Actual current-reader revocation, historical retained outcomes, publication projection bounds and nonrecursive ledger behavior likewise require concrete database-backed owners and are not claimed by these unit checks.

Executed final command:

`go test ./internal/turn -run '^TestReceiptUnit_' -short -race -cover -v`

Actual passing output, final lines:

```text
PASS
coverage: 0.6% of statements
ok  	github.com/cloudvane/litefactory/internal/turn	1.783s	coverage: 0.6% of statements
```

The 0.6% figure covers the complete turn package under this deliberately narrow test selection; it is not family or full-suite coverage. No acceptance assessment, factory submission, authentic host verification or remote publication performed.
