package turn

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestActivityService_PermissionMaterialBindsAttempt(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		change func(*store.ResultBinding)
	}{
		{"task", func(b *store.ResultBinding) { b.Task = "github:owner/repo#2" }},
		{"run", func(b *store.ResultBinding) { b.RunID++ }},
		{"attempt", func(b *store.ResultBinding) { b.Attempt = "replacement" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			binding := store.ResultBinding{Task: "github:owner/repo#1", RunID: 1, Attempt: "current"}
			material := func(b store.ResultBinding) []byte {
				t.Helper()
				// Construction and permission inspection must not perform I/O.
				receipts, err := NewReceiptService(&store.DB{}, b, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				s, err := NewActivityService(ActivityOptions{Receipts: receipts, Now: at})
				if err != nil {
					t.Fatal(err)
				}
				before, err := s.PermissionMaterial()
				if err != nil {
					t.Fatal(err)
				}
				var versioned struct{ Version string }
				if err := json.Unmarshal(before, &versioned); err != nil {
					t.Fatal(err)
				}
				if versioned.Version == "" {
					t.Fatal("advisory permission contract is unversioned")
				}
				if _, err := s.Lifecycle(ActivityLifecycle{Now: at, Clear: true}); err != nil {
					t.Fatal(err)
				}
				after, err := s.PermissionMaterial()
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(before, after) {
					t.Fatal("display lifecycle changed frozen permission material")
				}
				return before
			}
			before := material(binding)
			tc.change(&binding)
			if bytes.Equal(before, material(binding)) {
				t.Fatal("different authority scope reused permission material")
			}
		})
	}
}

func TestActivityService_LifecycleRejectsMissingTimeAtomically(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		input ActivityLifecycle
	}{
		{"read", ActivityLifecycle{}},
		{"terminal", ActivityLifecycle{Terminal: true}},
		{"clear", ActivityLifecycle{Clear: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			at := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
			s, _ := activityUnitService(t)
			before, err := s.Lifecycle(ActivityLifecycle{Now: at})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Lifecycle(tc.input); err == nil {
				t.Fatal("missing factory time accepted")
			}
			after, err := s.Lifecycle(ActivityLifecycle{Now: at})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) || s.terminal {
				t.Fatalf("rejected lifecycle changed activity: before=%+v after=%+v", before, after)
			}
		})
	}
}
