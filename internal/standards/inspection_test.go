package standards

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

// publicationAfterApprovalRead deterministically models a publication committed
// between inspection's approval read and its immutable history read.
type publicationAfterApprovalRead struct {
	Store
	publish func() error
}

func (s *publicationAfterApprovalRead) GetStandardsApproval(ctx context.Context, id string, version int64) (store.StandardsApproval, error) {
	a, err := s.Store.GetStandardsApproval(ctx, id, version)
	if errors.Is(err, store.ErrStandardsApprovalRequired) && s.publish != nil {
		publish := s.publish
		s.publish = nil
		if publishErr := publish(); publishErr != nil {
			return store.StandardsApproval{}, publishErr
		}
	}
	return a, err
}

func TestInspectIncludesApprovalWhenPublicationFollowsApprovalRead(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "inspection.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	writer := Service{Store: db, OwnerID: 42}
	p, err := writer.Create(ctx, "inspection", 0, []Operation{{Kind: "category.add", Category: &Category{ID: "c", Name: "Category"}}})
	if err != nil {
		t.Fatal(err)
	}
	wrapped := &publicationAfterApprovalRead{Store: db}
	wrapped.publish = func() error {
		if _, approvalErr := writer.Approve(ctx, p.ProposalID, p.Version, p.ChangeDigest); approvalErr != nil {
			return approvalErr
		}
		_, publishErr := writer.Publish(ctx, p.ProposalID, p.Version, p.ChangeDigest)
		return publishErr
	}
	reader := Service{Store: wrapped}
	inspection, err := reader.Inspect(ctx, p.ProposalID, p.Version)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Publication == nil || inspection.Approval == nil {
		t.Fatalf("incomplete published inspection: %#v", inspection)
	}
	if !reflect.DeepEqual(*inspection.Approval, inspection.Publication.Approval) {
		t.Fatalf("approval differs from retained publication: %#v", inspection)
	}
	if inspection.Approval.ProposalID != p.ProposalID || inspection.Approval.Version != p.Version || inspection.Approval.Owner != "42" {
		t.Fatalf("wrong approval: %#v", inspection.Approval)
	}
}
