# US-004 guidance engineering evidence

Frozen revision: `78f68c0840c660116a0b8110cf4e7a8e4bb7db719e5310d191c6025e2469ba2d`.

Executed `go test ./internal/turn -run '^TestMCPGuidance_' -short -race -cover -v` successfully. Actual output: `us004-guidance-check.log`. Six top-level tests and 17 named subtests passed; zero failed. Scoped package statement coverage is 0.8%; this is not whole-package coverage. An earlier compile failure in the concurrently edited lifecycle test is preserved in `us004-guidance-check-initial-build-failure.log`; that file was fixed by its owner before the passing run.

Pure tests cover absent registration, missing bound service, ten conditional catalog cases, five mode restrictions, distinct application/checkpoint operations, one parseable checkpoint contract for engineering/Validate, retained snapshot and criteria, no invented factory fields, and refusal of missing checkpoint scope. Validity is checked with production `agent.ParseContribution`. Initial application and trusted update-baseline tests require the concrete service's real store, and are owned by the lifecycle integration test specialist.

| In-memory render | Words | UTF-8 bytes |
| --- | ---: | ---: |
| Shared instructions | 160 | 1124 |
| Implement, all granted operation cards, excluding JSON contract | 376 | 3182 |
| Implement, including JSON contract and fences | 388 | 3332 |
| Validate, all granted operation cards, excluding JSON contract | 364 | 3065 |
| Validate, including JSON contract and fences | 397 | 4036 |

Counts use Go `strings.Fields` for words and byte length for UTF-8 bytes. The complete catalog is a constructed renderer input exercising every card, not an assertion that every operation is installed in either real mode. The single engineering example is 138 bytes and explicitly blocked; the Validate example is 959 bytes with two supplied criteria, three reviews, and unverified findings. Targets of 150–200 shared words and 300–450 combined words excluding the payload contract are met for these full-card cases; conditional smaller catalogs naturally produce shorter instructions. Application-mode metrics are covered separately with the real-store examples.

Guidance is provider-neutral at this seam. This test does not establish actual Claude/Codex rendered-token parity or host interpretation. Actual provider-rendered token measurements, including schemas/examples, are unavailable: no authentic captures or provider telemetry were supplied and no live host was acquired. No tokenizer estimate, billed-token claim, or savings claim is made. Codex host injection remains deferred under US-003.
