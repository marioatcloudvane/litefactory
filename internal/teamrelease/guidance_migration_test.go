package teamrelease

import (
	"encoding/json"
	"os"
	"testing"
)

// Compatibility assertions follow migrated advice into its review input while
// continuing to check executable contracts in the captured stack resources.
func migrationStandardBody(t *testing.T, id string) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../docs/standards-migration/proposal.json")
	if err != nil {
		t.Fatal(err)
	}
	var input struct {
		Operations []struct {
			Standard *struct {
				ID   string `json:"id"`
				Body string `json:"body"`
			} `json:"standard"`
		} `json:"operations"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	for _, op := range input.Operations {
		if op.Standard != nil && op.Standard.ID == id {
			return []byte(op.Standard.Body)
		}
	}
	t.Fatalf("missing migration standard %s", id)
	return nil
}
