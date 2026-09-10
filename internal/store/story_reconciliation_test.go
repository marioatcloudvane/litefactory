package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func reconciliationProjectionFixture(t *testing.T, db *DB) SubmissionProjection {
	t.Helper()
	p := SubmissionProjection{Parent: "github:owner/project#207", Generation: 1, Story: "US-007", IssueID: 221, ContentVersion: 2, Title: "Story", Body: "String must be x", Content: []byte(`{"id":"AC-001","intent":"create"}`), Criteria: []string{"AC-001"}}
	if err := db.PutManagedFeature(t.Context(), ManagedFeature{ParentTask: p.Parent, Generation: 1, State: "planning", WorkflowVersion: WorkflowSeparated}); err != nil {
		t.Fatal(err)
	}
	if err := db.BindFeatureContract(t.Context(), FeatureContract{Parent: p.Parent, Generation: 1, ContentVersion: 2, SubmissionVersion: 2}); err != nil {
		t.Fatal(err)
	}
	p.Revision = submissionTestDigest([]byte(p.Title + "\x00" + p.Body))
	if err := db.PutSubmissionProjection(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

func reconciliationNextGeneration(t *testing.T, db *DB, parent string) {
	t.Helper()
	if err := db.CompareManagedFeature(t.Context(), ManagedFeature{ParentTask: parent, Generation: 1, State: "implementing"}, "planning"); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplanManagedFeature(t.Context(), parent, 1); err != nil {
		t.Fatal(err)
	}
	if err := db.BindFeatureContract(t.Context(), FeatureContract{Parent: parent, Generation: 2, ContentVersion: 2, SubmissionVersion: 2}); err != nil {
		t.Fatal(err)
	}
}

func TestStoryReconciliationProjectionLookupIsRevisionAndGenerationBound(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	old := reconciliationProjectionFixture(t, db)
	reconciliationNextGeneration(t, db, old.Parent)
	got, err := db.FindSubmissionProjection(t.Context(), old.Parent, old.Story, old.Revision, 2)
	if err != nil || got.Generation != 1 || string(got.Content) != string(old.Content) {
		t.Fatalf("historical lookup: %+v %v", got, err)
	}
	current := old
	current.Generation = 2
	current.Content = []byte(`{"id":"AC-001","intent":"unchanged"}`)
	if err := db.PutSubmissionProjection(t.Context(), current); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		generation int
		content    []byte
	}{{1, old.Content}, {2, current.Content}} {
		got, err := db.FindSubmissionProjection(t.Context(), old.Parent, old.Story, old.Revision, tc.generation)
		if err != nil || got.Generation != tc.generation || string(got.Content) != string(tc.content) {
			t.Fatalf("generation %d got %+v %v", tc.generation, got, err)
		}
	}
	for _, tc := range []struct {
		parent, story, revision string
		generation              int
	}{{"github:other/project#207", old.Story, old.Revision, 2}, {old.Parent, "US-008", old.Revision, 2}, {old.Parent, old.Story, submissionTestDigest([]byte("unobserved revision")), 2}, {old.Parent, old.Story, old.Revision, 0}} {
		if _, err := db.FindSubmissionProjection(t.Context(), tc.parent, tc.story, tc.revision, tc.generation); !errors.Is(err, ErrNotFound) {
			t.Fatalf("cross-boundary lookup returned %v for %+v", err, tc)
		}
	}
	conflict := FeatureContract{Parent: old.Parent, Generation: 1, ContentVersion: 1, SubmissionVersion: 1}
	if err := db.BindFeatureContract(t.Context(), conflict); err == nil {
		t.Fatal("historical generation contract changed")
	}
	feature, err := db.GetManagedFeature(t.Context(), old.Parent)
	if err != nil || feature.WorkflowVersion != WorkflowSeparated || feature.Ready {
		t.Fatalf("lookup/replan altered workflow/readiness: %+v %v", feature, err)
	}
}

func TestStoryReconciliationRetirementSurvivesReplanAndRestart(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "history.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	old := reconciliationProjectionFixture(t, db)
	reconciliationNextGeneration(t, db, old.Parent)
	current := old
	current.Generation = 2
	current.Body = "String must be y"
	current.Revision = submissionTestDigest([]byte(current.Title + "\x00" + current.Body))
	current.Content = []byte(`{"id":"AC-002","intent":"update"}`)
	current.Criteria = []string{"AC-002"}
	current.RetiredCriteria = []string{"AC-001"}
	if err := db.PutSubmissionProjection(t.Context(), current); err != nil {
		t.Fatal(err)
	}
	before, err := db.ListCriterionHistory(t.Context(), old.Parent, old.Story)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 2 || before[0].RetiredRevision != current.Revision || before[0].FirstRevision != old.Revision {
		t.Fatalf("incorrect retirement: %+v", before)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := db.FindSubmissionProjection(t.Context(), old.Parent, old.Story, old.Revision, 2)
	if err != nil || got.Generation != 1 || got.Body != old.Body || string(got.Content) != string(old.Content) {
		t.Fatalf("historical bytes lost after retirement: %+v %v", got, err)
	}
	// Replaying immutable old evidence remains valid even after the ID retires.
	if err := db.PutSubmissionProjection(t.Context(), old); err != nil {
		t.Fatal(err)
	}
	reuse := current
	reuse.Body = "Attempt to recycle retired ID"
	reuse.Revision = submissionTestDigest([]byte(reuse.Title + "\x00" + reuse.Body))
	reuse.Criteria = []string{"AC-001"}
	reuse.RetiredCriteria = nil
	if err := db.PutSubmissionProjection(t.Context(), reuse); err == nil {
		t.Fatal("retired ID reused in next generation")
	}
	if _, err := db.FindSubmissionProjection(t.Context(), old.Parent, old.Story, reuse.Revision, 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rejected projection leaked through rollback: %v", err)
	}
	after, err := db.ListCriterionHistory(t.Context(), old.Parent, old.Story)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("failed reuse changed history: %+v %v", after, err)
	}
}
