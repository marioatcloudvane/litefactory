# Run 0015 — one-off inventory fixture checks

Scope: US-009, issue 306, frozen revision `ef612f5b50be87b3692b0d38b645e4eef2d87e61e1a5db067e8aab5e211cefb7`. Tests exercise the session-local `legacy_inventory.go`; no product source or daemon database was changed. All fixture databases are isolated temporary SQLite files created with existing `store.Open`/`Migrate` and populated with existing store accessors. Tests make no forge calls.

Four tests passed, zero failed, both plain and race executions:

- `TestInventoryMissingDatabase`: failed reads report errors, retain all three reader dispositions, and do not create the missing file.
- `TestInventoryEmptyDatabase`: empty inventory remains bounded evidence; three readers still require retention.
- `TestInventoryLegacyAndPendingDelivery`: a real legacy owner retains task/generation/contract/workflow/story identity; an uncertain report remains visible and pending; retained body/marker text stays out of JSON; inventory does not upgrade persisted contract versions or complete the delivery.
- `TestInventoryUnmigratedDatabase`: missing schema cannot become a successful inventory or reader-removal permission.

Commands executed from the repository root (the two file arguments are these files under the session directory):

```text
go test -race -cover -v -timeout 2m <session>/legacy_inventory.go <session>/legacy_inventory_test.go
go test -v -timeout 2m <session>/legacy_inventory.go <session>/legacy_inventory_test.go
```

Actual race result:

```text
--- PASS: TestInventoryMissingDatabase (0.01s)
--- PASS: TestInventoryUnmigratedDatabase (0.04s)
--- PASS: TestInventoryLegacyAndPendingDelivery (8.08s)
--- PASS: TestInventoryEmptyDatabase (10.70s)
PASS
coverage: 64.9% of statements
ok  command-line-arguments 12.366s coverage: 64.9% of statements
```

Actual plain result:

```text
--- PASS: TestInventoryMissingDatabase (0.01s)
--- PASS: TestInventoryUnmigratedDatabase (0.04s)
--- PASS: TestInventoryEmptyDatabase (0.70s)
--- PASS: TestInventoryLegacyAndPendingDelivery (0.82s)
PASS
ok  command-line-arguments 1.232s
```

These checks establish offline script behavior only. They do not establish complete runtime cohort coverage, live drain, authorized migration, authentic host observations, or US-009 engineering completion. CLI smoke checks and live read-only observation are owned by the implementation engineer. Real upgrade/completion/admission receipts remain external prerequisites for reader removal.
