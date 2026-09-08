# Run 15 — deterministic delivery tests

Scope: US-3 delivery/lifecycle, US-4 mode isolation, US-5 durable provenance. Tests derive from story acceptance criteria. Scratch directories, fake processes and migrated scratch SQLite fixtures follow the approved project plan; no public network, paid CLI, build tags or live LF_HOME used. Production files are not changed by this testing pass.

## Added coverage

- Native Claude/Codex roles use the locally resolved model, preserve file-backed body, render deterministically and reject unsafe role names/unknown vendors.
- Codex forces configuration/rules isolation even when operator user-config loading was otherwise enabled; native role overrides reference bundle files. Claude uses isolated plugin and file-backed bootstrap arguments.
- Missing central team does not prevent catalog-free classification; classifier receives no team selection or worktree context.
- Selected snapshot yields exactly its ordered roster and resolved-model/version provenance. Identity matrix covers stable identity and changes to source URL, release version, manifest digest, team, mode, model, body and override provenance.
- Retained bundle verification accepts exact files and rejects changed, missing, extra and symlinked resources.
- Eight concurrent bundle publications have distinct verified roots.
- Real scratch SQLite proves equal identity retains the same segment/root; changed release version creates a distinct segment/root while old resources remain valid; missing retained files prevent resume.
- Delivery cloning and serialized run metadata retain independent inline source/version/manifest, effective identity, specialist/model, source-byte and resource-hash evidence without reading the bundle.

## Results

First deterministic pass found one failure: `TestPlanDeliverySelectedRosterAndIdentity/override`, `internal/turn/delivery_test.go`. Changing `CapturedAgent.Override` from false to true did not change effective identity. Expected: override provenance participates in identity under AC-3.5. Actual: identity remained equal. Root orchestrator owns production triage/fix; regression remains enabled.

Bug cycle 1: the runtime engineer included delivery evidence in identity serialization. Independent rerun `go test -race -cover ./internal/turn -run '^TestPlanDeliverySelectedRosterAndIdentity$' -timeout 60s` passes the entire identity matrix (1.726s, targeted coverage 3.7%). The regression remains enabled. Across the focused executions, all 11 new/updated top-level tests now pass; no unresolved new-test failure remains.

Native renderer, neutral-agent, Codex, harness and store full package race runs passed, with coverage 93.3%, 56.0%, 43.7%, 82.9%, and 74.7% respectively. Targeted segment retention/rotation, eight concurrent bundles, all five bundle integrity cases and catalog-free classification passed. Targeted turn coverage was 8.7%, daemon 0.3%; these are not full-suite coverage claims. The initial broad turn run completed in 256.524s, coverage 78.7%, with only the subsequently fixed identity failure.

The last broad command used an insufficient `-timeout 120s`: both turn and daemon exceeded two minutes amid existing parallel Git/SQLite fixtures. This is recorded as an incomplete broad verification, not a clean full-suite pass or evidence of a delivery-specific regression. The root orchestrator owns the final whole-repository verification with an appropriate timeout. No testing subprocess remains active from this pass.

## Not established by these tests

- AC-3.7 actual native delegated execution, exact CLI-version tool/model enforcement, authentication and concurrency require suitable-environment real CLI evidence. Fake-process/argv tests do not establish those vendor semantics.
- AC-5.2 measured token improvements remain open; byte evidence is not token savings.
- This pass does not independently prove every transitive skill resource, override resource mutation, bootstrap revision, parent-auth setting, source acquisition, catalog publication, or permission ceiling. Distribution/CLI coverage belongs to the separate test pass; existing auth/permission regression suites remain relevant but are not new live delivery proof.
- No artifact cleanup subsystem is introduced or exercised; metadata independence and retained-resource safety are tested directly.
