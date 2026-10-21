package standards_test

import (
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestExceptionPolicyPublicationRetainsExactReviewedPolicy(t *testing.T) {
	first, second, _ := publicationServices(t)
	policy := &store.StandardsExceptionPolicy{Version: 1, Mode: "allow-task", RepositoryIDs: []int64{9, 2}, MaxLifetimeSeconds: 3600}
	ops := append(addCategory("c", "Category"), standards.Operation{Kind: "standard.add", Standard: &standards.Standard{ID: "s", Title: "Standard", Body: "Guidance", CategoryIDs: []string{"c"}, ExceptionPolicy: policy}})
	p, err := first.Create(t.Context(), "policy", 0, ops)
	if err != nil {
		t.Fatal(err)
	}
	review, err := second.Review(t.Context(), p.ProposalID, p.Version)
	if err != nil {
		t.Fatal(err)
	}
	want := &store.StandardsExceptionPolicy{Version: 1, Mode: "allow-task", RepositoryIDs: []int64{2, 9}, MaxLifetimeSeconds: 3600}
	if !reflect.DeepEqual(review.After.Standards[0].ExceptionPolicy, want) {
		t.Fatalf("review policy: %+v", review.After.Standards[0].ExceptionPolicy)
	}
	approvePublication(t, first, p)
	published, err := first.Publish(t.Context(), p.ProposalID, p.Version, p.ChangeDigest)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := second.Revision(t.Context(), published.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(retained.Snapshot.Standards[0].ExceptionPolicy, want) {
		t.Fatalf("retained policy: %+v", retained.Snapshot.Standards[0].ExceptionPolicy)
	}
	if err := standards.VerifyRevision(retained); err != nil {
		t.Fatal(err)
	}
	retained.Snapshot.Standards[0].ExceptionPolicy.MaxLifetimeSeconds++
	if err := standards.VerifyRevision(retained); err == nil {
		t.Fatal("changed policy verified against approved digest")
	}
}
