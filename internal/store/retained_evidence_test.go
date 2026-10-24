package store

import (
	"errors"
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/retainedevidence"
)

func TestEvidenceManifestRevisionIsImmutable(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	m := retainedevidence.Manifest{
		Version:  retainedevidence.Version,
		Scope:    retainedevidence.Scope{Kind: "workflow", ID: "attempt-1", Repository: "repo-1", Task: "task-1"},
		Revision: "revision-1", Coverage: retainedevidence.Complete,
		Entries: []retainedevidence.Entry{{Kind: "command", OwnerID: "run-1", Revision: "capture-1", Bytes: 3, Digest: retainedevidence.Digest([]byte("old")), Capture: retainedevidence.Complete, Availability: retainedevidence.Available}},
	}
	for range 2 {
		if err := db.PutEvidenceManifest(t.Context(), m); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.EvidenceManifest(t.Context(), m.Scope, m.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, m) {
		t.Fatalf("retained manifest = %+v, want %+v", got, m)
	}
	m.Entries[0].Digest = retainedevidence.Digest([]byte("new"))
	if err := db.PutEvidenceManifest(t.Context(), m); !errors.Is(err, retainedevidence.ErrConflict) {
		t.Fatalf("changed manifest error = %v", err)
	}
	m.Revision = "revision-2"
	if err := db.PutEvidenceManifest(t.Context(), m); !errors.Is(err, retainedevidence.ErrConflict) {
		t.Fatalf("changed source with same source revision error = %v", err)
	}
	if _, err := db.EvidenceManifest(t.Context(), m.Scope, m.Revision); !errors.Is(err, ErrNotFound) {
		t.Fatalf("conflicting transaction left a manifest: %v", err)
	}
	m.Entries[0].Revision = "capture-2"
	if err := db.PutEvidenceManifest(t.Context(), m); err != nil {
		t.Fatal(err)
	}
	old, err := db.EvidenceManifest(t.Context(), m.Scope, "revision-1")
	if err != nil {
		t.Fatal(err)
	}
	if old.Entries[0].Digest != retainedevidence.Digest([]byte("old")) {
		t.Fatal("new revision replaced old source")
	}
	otherScope := m.Scope
	otherScope.Repository = "other-repository"
	if _, err := db.EvidenceManifest(t.Context(), otherScope, m.Revision); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-repository identity matched: %v", err)
	}
}
