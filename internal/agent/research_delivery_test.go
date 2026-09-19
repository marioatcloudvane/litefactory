package agent

import "testing"

func TestMainRoleDeliveryValidation(t *testing.T) {
	for _, tc := range []struct {
		name, role, model string
		roles             []string
		valid             bool
	}{
		{"main", "research", "worker", []string{"research"}, true},
		{"legacy", "", "", []string{"research"}, true},
		{"missing model", "research", "", []string{"research"}, false},
		{"extra role", "research", "worker", []string{"research", "writer"}, false},
		{"unsafe", "../research", "worker", []string{"../research"}, false},
		{"orphan model", "", "worker", []string{"research"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := Delivery{Root: t.TempDir(), Identity: "bound", MainRole: tc.role, MainModel: tc.model, Roles: tc.roles}
			if (d.validate() == nil) != tc.valid {
				t.Fatalf("unexpected validation: %v", d.validate())
			}
		})
	}
}
