package standards_test

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

func publicationServices(t *testing.T) (*standards.Service, *standards.Service, *store.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "standards.db")
	open := func() *store.DB {
		t.Helper()
		db, err := store.Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := db.Close(); err != nil {
				t.Error(err)
			}
		})
		return db
	}
	first := open()
	if err := first.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	second := open()
	return &standards.Service{Store: first, OwnerID: 71}, &standards.Service{Store: second, OwnerID: 71}, first
}

func addCategory(id, name string) []standards.Operation {
	return []standards.Operation{{Kind: "category.add", Category: &standards.Category{ID: id, Name: name}}}
}

func approvePublication(t *testing.T, svc *standards.Service, p store.StandardsProposalVersion) {
	t.Helper()
	if _, err := svc.Approve(t.Context(), p.ProposalID, p.Version, p.ChangeDigest); err != nil {
		t.Fatal(err)
	}
}

func TestPublicationRequiresFreshApprovalAfterRestoredText(t *testing.T) {
	t.Parallel()
	first, second, db := publicationServices(t)
	ctx := t.Context()
	p, err := first.Create(ctx, "restored", 0, addCategory("quality", "Quality"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Approve(ctx, p.ProposalID, p.Version, "wrong"); !errors.Is(err, store.ErrStandardsConflict) {
		t.Fatalf("wrong reviewed digest approved: %v", err)
	}
	ownerless := &standards.Service{Store: db}
	if _, err := ownerless.Approve(ctx, p.ProposalID, p.Version, p.ChangeDigest); !errors.Is(err, standards.ErrOwnerRequired) {
		t.Fatalf("ownerless approval: %v", err)
	}
	if _, err := ownerless.Publish(ctx, p.ProposalID, p.Version, p.ChangeDigest); !errors.Is(err, standards.ErrOwnerRequired) {
		t.Fatalf("ownerless publication: %v", err)
	}
	approvePublication(t, first, p)
	edited, err := second.Edit(ctx, p.ProposalID, p.Version, 0, addCategory("quality", "Changed"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Publish(ctx, p.ProposalID, p.Version, p.ChangeDigest); !errors.Is(err, store.ErrStandardsConflict) {
		t.Fatalf("old approved version published after edit: %v", err)
	}
	if _, err := second.Publish(ctx, edited.ProposalID, edited.Version, edited.ChangeDigest); !errors.Is(err, store.ErrStandardsApprovalRequired) {
		t.Fatalf("edited version inherited approval: %v", err)
	}
	restored, err := first.Edit(ctx, p.ProposalID, edited.Version, 0, addCategory("quality", "Quality"))
	if err != nil {
		t.Fatal(err)
	}
	if restored.ChangeDigest != p.ChangeDigest || restored.ResultDigest != p.ResultDigest || restored.Version == p.Version {
		t.Fatalf("fixture did not restore identical content in a new version: %+v", restored)
	}
	if _, err := first.Publish(ctx, restored.ProposalID, restored.Version, restored.ChangeDigest); !errors.Is(err, store.ErrStandardsApprovalRequired) {
		t.Fatalf("restored text reused old version approval: %v", err)
	}
	if _, err := second.Current(ctx); !errors.Is(err, store.ErrStandardsUnpublished) {
		t.Fatalf("failed publication moved head: %v", err)
	}
	oldApproval, err := db.GetStandardsApproval(ctx, p.ProposalID, p.Version)
	if err != nil || oldApproval.ChangeDigest != p.ChangeDigest {
		t.Fatalf("old approval was not retained: %+v %v", oldApproval, err)
	}
	inspection, err := second.Inspect(ctx, p.ProposalID, p.Version)
	if err != nil || inspection.Approval == nil || !reflect.DeepEqual(*inspection.Approval, oldApproval) || !reflect.DeepEqual(inspection.Proposal, p) || inspection.Publication != nil {
		t.Fatalf("old exact approval inspection was lost after edits: %+v %v", inspection, err)
	}
	unapproved, err := second.Inspect(ctx, restored.ProposalID, restored.Version)
	if err != nil || unapproved.Approval != nil {
		t.Fatalf("inspection substituted prior approval for restored version: %+v %v", unapproved, err)
	}
	approvePublication(t, second, restored)
	if _, err := first.Publish(ctx, restored.ProposalID, restored.Version, restored.ChangeDigest); err != nil {
		t.Fatal(err)
	}
}

func TestPublicationRebaseRetryAndHistoricalImmutability(t *testing.T) {
	t.Parallel()
	first, second, _ := publicationServices(t)
	ctx := t.Context()
	create := func(svc *standards.Service, id string, base int64, ops []standards.Operation) store.StandardsProposalVersion {
		t.Helper()
		p, err := svc.Create(ctx, id, base, ops)
		if err != nil {
			t.Fatal(err)
		}
		approvePublication(t, svc, p)
		return p
	}
	one := create(first, "first", 0, addCategory("quality", "Quality"))
	stale := create(second, "second", 0, addCategory("safety", "Safety"))
	r1, err := first.Publish(ctx, one.ProposalID, one.Version, one.ChangeDigest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Publish(ctx, stale.ProposalID, stale.Version, stale.ChangeDigest); !errors.Is(err, store.ErrStandardsConflict) {
		t.Fatalf("stale base silently merged: %v", err)
	}
	rebased, err := second.Edit(ctx, stale.ProposalID, stale.Version, r1.ID, addCategory("safety", "Safety"))
	if err != nil {
		t.Fatal(err)
	}
	if rebased.ChangeDigest == stale.ChangeDigest || rebased.BaseRevision != r1.ID {
		t.Fatalf("rebase did not bind new base: %+v", rebased)
	}
	if _, err := second.Publish(ctx, rebased.ProposalID, rebased.Version, rebased.ChangeDigest); !errors.Is(err, store.ErrStandardsApprovalRequired) {
		t.Fatalf("rebase reused old approval: %v", err)
	}
	approvePublication(t, first, rebased)
	r2, err := second.Publish(ctx, rebased.ProposalID, rebased.Version, rebased.ChangeDigest)
	if err != nil {
		t.Fatal(err)
	}
	if r2.ParentRevision != r1.ID || len(r2.Snapshot.Categories) != 2 {
		t.Fatalf("publication lost complete parent snapshot: %+v", r2)
	}
	retry, err := second.Publish(ctx, one.ProposalID, one.Version, one.ChangeDigest)
	if err != nil || !reflect.DeepEqual(retry, r1) {
		t.Fatalf("retry after head moved did not recover original revision: %+v %v", retry, err)
	}
	historical, err := first.Revision(ctx, r1.ID)
	if err != nil || !reflect.DeepEqual(historical, r1) {
		t.Fatalf("later publication rewrote history: %+v %v", historical, err)
	}
	if _, err := first.Edit(ctx, one.ProposalID, one.Version, r2.ID, addCategory("other", "Other")); !errors.Is(err, store.ErrStandardsConflict) {
		t.Fatalf("published proposal remained editable: %v", err)
	}
	changes, err := first.Diff(ctx, r1.ID, r2.ID)
	if err != nil || len(changes) != 1 || changes[0].Entity != "category" || changes[0].ID != "safety" || changes[0].Kind != "add" {
		t.Fatalf("historical diff = %+v %v", changes, err)
	}
	// Repeating the same retained approval recovers its original record even
	// when a later publication has advanced the book head.
	inspection, err := first.Inspect(ctx, one.ProposalID, one.Version)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := second.Approve(ctx, one.ProposalID, one.Version, one.ChangeDigest)
	if err != nil || inspection.Approval == nil || !reflect.DeepEqual(approval, *inspection.Approval) {
		t.Fatalf("approval retry changed retained approval: %+v %v", approval, err)
	}
	current, err := first.Current(ctx)
	if err != nil || !reflect.DeepEqual(current, r2) {
		t.Fatalf("retry or failed edit moved current book: %+v %v", current, err)
	}
}

func TestPublicationRejectsCorruptRetainedDigests(t *testing.T) {
	t.Parallel()
	first, _, db := publicationServices(t)
	ctx := t.Context()
	valid, err := first.Create(ctx, "valid", 0, addCategory("quality", "Quality"))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"change", "result"} {
		t.Run(field, func(t *testing.T) {
			// Store access deliberately bypasses the service's canonicalization to
			// model retained data written with an incorrect digest, without SQL
			// outside the store package or disabling immutability protections.
			corrupt := valid
			corrupt.ProposalID = "corrupt-" + field
			if field == "change" {
				corrupt.ChangeDigest = "incorrect"
			} else {
				corrupt.ResultDigest = "incorrect"
			}
			corrupt, err := db.SaveStandardsProposal(t.Context(), corrupt, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := first.Review(t.Context(), corrupt.ProposalID, corrupt.Version); !errors.Is(err, store.ErrStandardsCorrupt) {
				t.Fatalf("corrupt %s digest reviewed: %v", field, err)
			}
			if _, err := first.Approve(t.Context(), corrupt.ProposalID, corrupt.Version, corrupt.ChangeDigest); !errors.Is(err, store.ErrStandardsCorrupt) {
				t.Fatalf("corrupt %s digest approved: %v", field, err)
			}
			if _, err := first.Publish(t.Context(), corrupt.ProposalID, corrupt.Version, corrupt.ChangeDigest); !errors.Is(err, store.ErrStandardsCorrupt) {
				t.Fatalf("corrupt %s digest published: %v", field, err)
			}
		})
	}
	listed, err := first.List(ctx)
	if listed != nil || !errors.Is(err, store.ErrStandardsCorrupt) {
		t.Fatalf("corrupt proposals returned a successful list: %#v, %v", listed, err)
	}
	if !strings.Contains(err.Error(), "corrupt-change") || !strings.Contains(err.Error(), "corrupt-result") {
		t.Fatalf("list omitted an independent corrupt proposal: %v", err)
	}
	if _, err := first.Current(ctx); !errors.Is(err, store.ErrStandardsUnpublished) {
		t.Fatalf("corrupt publication changed unpublished state: %v", err)
	}
}
