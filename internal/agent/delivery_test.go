package agent

import "testing"

func TestDeliveryCloneOwnsAllMutableEvidence(t *testing.T) {
	t.Parallel()
	original := Delivery{Roles: []string{"reviewer"}, Skills: []string{"workflow"}, Evidence: DeliveryEvidence{Specialists: []SpecialistEvidence{{ID: "reviewer"}}, ResourceHashes: map[string]string{"agents/reviewer.md": "verified"}}}
	clone := original.Clone()
	clone.Roles[0] = "other"
	clone.Skills[0] = "other"
	clone.Evidence.Specialists[0].ID = "other"
	clone.Evidence.ResourceHashes["agents/reviewer.md"] = "changed"
	if original.Roles[0] != "reviewer" || original.Skills[0] != "workflow" || original.Evidence.Specialists[0].ID != "reviewer" || original.Evidence.ResourceHashes["agents/reviewer.md"] != "verified" {
		t.Fatal("clone aliases provenance or selected resources")
	}
}
