package store

import "testing"

func TestConflictProposalInputsRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name   string
		inputs ConflictProposalInputs
	}{
		{"historical", ConflictProposalInputs{}},
		{"base-absent-task", ConflictProposalInputs{Observed: true, TargetPhase: "base", TargetRef: "origin/main", BaseRef: "origin/main", BaseSHA: "base", TaskRef: "origin/task"}},
		{"task-present", ConflictProposalInputs{Observed: true, TargetPhase: "task", TargetRef: "origin/task", BaseRef: "origin/main", BaseSHA: "base", TaskRef: "origin/task", TaskSHA: "operator"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := newTestDB(t)
			session := testSession(t, db, 209)
			targetSHA := tc.inputs.BaseSHA
			if tc.inputs.TargetPhase == "task" {
				targetSHA = tc.inputs.TaskSHA
			}
			if targetSHA == "" {
				targetSHA = "legacy"
			}
			p, created, err := db.CreateOrGetConflictProposal(t.Context(), CreateConflictProposalParams{SessionID: session.ID, TriggerCommentID: 10, Model: "codex", Format: ConflictRegionFormat, BaseSHA: targetSHA, BranchSHA: "local", Inputs: tc.inputs, Files: []ConflictProposalFile{{Path: "a.go", Details: ConflictFileDetails{OriginalDigest: "original", ResultDigest: "result", Mode: "100644", Regions: []ConflictRegionResolution{{ID: "region", Resolution: "resolved\n"}}}}}})
			if err != nil || !created {
				t.Fatalf("create %v %v", created, err)
			}
			got, err := db.GetConflictProposal(t.Context(), p.ID)
			if err != nil || got.Inputs != tc.inputs {
				t.Fatalf("lost binding: %+v %v", got, err)
			}
			replay, created, err := db.CreateOrGetConflictProposal(t.Context(), CreateConflictProposalParams{SessionID: session.ID, TriggerCommentID: 10, Model: "codex", Format: ConflictRegionFormat, BaseSHA: "replacement", BranchSHA: "local", Files: p.Files, Inputs: ConflictProposalInputs{Observed: true, TargetPhase: "task", TargetRef: "origin/task", TaskRef: "origin/task", TaskSHA: "replacement", BaseRef: "origin/main", BaseSHA: "base"}})
			if err != nil || created || replay.Inputs != tc.inputs {
				t.Fatalf("replay overwrote original approval: %+v %v", replay, err)
			}
		})
	}
}
