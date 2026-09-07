package daemon_test

import (
	"testing"
)

// TestSyncLabelReachesTurnRequest is T-7.2's wiring, exercised end to end:
// whatever the label ladder in internal/daemon/sync.go resolved is what
// [turn.Request.SyncOverride] carries once a turn dispatches (AC-7.1). A
// resolved label beats the configured default; anything that did not
// resolve — silence, or a refusal — leaves SyncOverride nil so the
// configured default applies unchanged, never a guessed value.
//
// This is the same style [TestConfiguredAttachmentOverrideReachesTurnRequest]
// uses for AttachmentsAllowed and [TestModesSelectCanonicalPermissionsForBothAgents]
// uses for Agent: a real dispatch through the fixture, with the executor
// capturing the turn.Request it was handed.
func TestSyncLabelReachesTurnRequest(t *testing.T) {
	tests := []struct {
		name            string
		labels          []string
		wantOverrideSet bool
		wantOverride    bool
	}{
		{
			name: "no Sync: label/SyncOverride stays nil, the config default applies",
		},
		{
			name:            "Sync: On/SyncOverride is a pointer to true",
			labels:          []string{"Sync: On"},
			wantOverrideSet: true,
			wantOverride:    true,
		},
		{
			name:            "Sync: Off/SyncOverride is a pointer to false",
			labels:          []string{"Sync: Off"},
			wantOverrideSet: true,
			wantOverride:    false,
		},
		{
			name:   "mis-cased prefix/refused, SyncOverride stays nil",
			labels: []string{"SYNC: On"},
		},
		{
			name:   "invalid value/refused, SyncOverride stays nil",
			labels: []string{"Sync: maybe"},
		},
		{
			name:   "duplicated label/ambiguous, SyncOverride stays nil",
			labels: []string{"Sync: On", "Sync: Off"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.plantCursor()
			f.issueWithLabels("Add a /health endpoint", tt.labels...)
			f.settle()
			f.forge.addComment(testIssue, 501, owner(), "/implement build")
			f.settle()

			calls := f.turns.calls()
			if len(calls) != 1 {
				t.Fatalf("executor calls = %d, want 1\nlogs:\n%s", len(calls), f.logs)
			}
			req := calls[0]

			if tt.wantOverrideSet {
				if req.SyncOverride == nil {
					t.Fatalf("SyncOverride = nil, want a pointer to %t", tt.wantOverride)
				}
				if *req.SyncOverride != tt.wantOverride {
					t.Fatalf("SyncOverride = %t, want %t", *req.SyncOverride, tt.wantOverride)
				}
				return
			}
			if req.SyncOverride != nil {
				t.Fatalf("SyncOverride = %t, want nil (the config default must apply, never a guessed value)",
					*req.SyncOverride)
			}
		})
	}
}
