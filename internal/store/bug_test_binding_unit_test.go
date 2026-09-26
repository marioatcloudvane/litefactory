package store_test

import (
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestBugTestCall_PhaseBinding(t *testing.T) {
	t.Parallel()
	base := store.BugCheckBinding{SessionID: 42, Operation: "parent-call", Generation: "parent-generation", Repair: "repair-2", Phase: "parent", SourceCommit: "source", SourceTree: "tree", TargetRepository: "owner/repo", TargetBranch: "main", TargetCommit: "target", RecipeDigest: "recipe", EnvironmentRevision: "env-2", PermissionRevision: "permissions-3"}
	for _, phase := range []string{"regression", "focused", "full"} {
		t.Run(phase+"/stable isolated binding", func(t *testing.T) {
			t.Parallel()
			call := store.BugTestCall{ID: 73, Generation: 2, Request: store.BugTestRequest{Binding: base}}
			want := base
			want.Operation = "bug-test:73:" + phase
			want.Generation = "bug-test:73"
			want.Phase = phase
			got := call.PhaseBinding(phase)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("phase binding = %+v, want %+v", got, want)
			}
			if call.Request.Binding != base || call.PhaseBinding(phase) != got {
				t.Fatal("binding changed original inputs or replay identity")
			}
			call.ID++
			next := call.PhaseBinding(phase)
			if next.Operation == got.Operation || next.Generation == got.Generation {
				t.Fatal("different child reused command or generation identity")
			}
		})
	}
}
