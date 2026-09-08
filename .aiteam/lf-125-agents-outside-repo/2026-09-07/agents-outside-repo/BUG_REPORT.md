# Bug cycle 1 — US-3 delivery identity

## Root cause: selected override provenance is recorded but excluded from identity

**Affected tests:** `TestPlanDeliverySelectedRosterAndIdentity/override`

**File(s):** `internal/turn/delivery.go:71`, `internal/turn/delivery.go:87`, `internal/turn/delivery.go:139`

**Explanation:** `planDelivery` copies `CapturedAgent.Override` into `p.evidence.Specialists`, but its final identity serialization includes only parent identity, renderer version, snapshot digest, vendor kind, rendered files and permission settings. The separate `provenance.json` file contains team/mode/snapshot/remote source/role IDs, not specialist override evidence. Consequently, changing only the selected specialist's override provenance changes neither the native renderer's input nor any field included in the hash. The identity stays unchanged and a previous conversation can be resumed despite changed override provenance, contrary to the approved identity contract. The test correctly isolates this omission by keeping source bytes and snapshot digest fixed.

**Suggested fix:** Add the already-built, typed `agent.DeliveryEvidence` value to the final identity serialization in `planDelivery`. This is the narrowest change: it includes selected override flags together with their existing source paths, hashes, tiers and model evidence without changing rendering or introducing a second provenance representation. Keep all existing identity inputs. Re-run the complete `TestPlanDeliverySelectedRosterAndIdentity` matrix to verify that unchanged evidence remains stable and override changes rotate identity. No production files changed during this analysis-only cycle.
