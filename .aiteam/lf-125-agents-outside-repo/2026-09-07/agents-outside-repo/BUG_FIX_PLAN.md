# Bug Fix Plan
# Source: BUG_REPORT.md
# Created: 2026-09-07
# Status: DRAFT

## Bug Tasks

| Task ID | Bug | Root Cause (summary) | Files Affected | Fix Approach | Agent | Status |
|---------|-----|----------------------|----------------|--------------|-------|--------|
| BT-001 | Selected specialist override provenance does not rotate delivery identity, violating approved AC3.5 and failing `TestPlanDeliverySelectedRosterAndIdentity/override`. | `planDelivery` records override flags in delivery evidence but omits that evidence from the final identity serialization. | `internal/turn/delivery.go`; regression coverage: `internal/turn/delivery_test.go:67` | Include the existing typed `agent.DeliveryEvidence` in the identity serialization, retaining all current identity inputs and native rendering behavior. Done when changing a selected specialist's override flag from false to true with unchanged source bytes and snapshot digest changes identity, unchanged evidence remains stable, and the complete `TestPlanDeliverySelectedRosterAndIdentity` matrix passes. | go-implementation-engineer | TODO |
