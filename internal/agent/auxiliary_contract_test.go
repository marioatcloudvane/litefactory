package agent

import "testing"

// Auxiliary contracts do not inherit application-result schema or authority.
func TestAuxiliaryContractsRejectApplicationInputs(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"unknown", `{"mode":"research"}`, `{"result":{"outcome":"completed","payload":{"research":{}}}}`} {
		t.Run("classifier/"+input, func(t *testing.T) {
			t.Parallel()
			if _, ok := ParseMode(input); ok {
				t.Fatal("unsupported classifier input accepted")
			}
		})
	}
	for _, test := range []struct{ name, input string }{
		{"version", `{"version":1,"items":[]}`},
		{"application", `{"result":{"outcome":"completed"},"items":[]}`},
		{"item authority", `{"items":[{"payload":"/implement task","due_at":"2026-09-15T12:00:00Z","approved":true}]}`},
	} {
		t.Run("scheduler/"+test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseScheduleProposal(test.input); err == nil {
				t.Fatal("unsupported schedule input accepted")
			}
		})
	}
	req := ConflictResolutionRequest{Branch: "task", Base: "main", Files: []ConflictFileContent{{Path: "file", Regions: []ConflictRegionContent{{ID: "region", Content: "conflict"}}}}}
	for _, test := range []struct{ name, input string }{
		{"version", `{"version":2,"files":[{"path":"file","regions":[{"id":"region","resolution":"fixed"}]}]}`},
		{"application", `{"result":{"outcome":"completed","payload":{"spike":{}}}}`},
		{"wrong file", `{"version":1,"files":[{"path":"other","regions":[{"id":"region","resolution":"fixed"}]}]}`},
		{"wrong region", `{"version":1,"files":[{"path":"file","regions":[{"id":"other","resolution":"fixed"}]}]}`},
	} {
		t.Run("conflict/"+test.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseRegionConflictResolution(test.input, req)
			if err == nil || len(got.Files) != 0 {
				t.Fatalf("unsupported response must fail without partial resolution: %+v, %v", got, err)
			}
		})
	}
}
