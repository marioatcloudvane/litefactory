package agent

import (
	"os"
	"testing"
)

// shippedRoleText reads a factory library role from the repository source.
func shippedRoleText(t *testing.T, role string) string {
	t.Helper()
	raw, err := os.ReadFile("../../teams/source/factory/roles/" + role + ".md")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
