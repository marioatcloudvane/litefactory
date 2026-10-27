package standards_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

// Only the two immutable read methods are supplied to the projection.
type bookReads struct {
	source   standards.PublishedStore
	revision func(*store.StandardsRevision)
	proposal func(*store.StandardsProposalVersion)
}

func (s bookReads) GetStandardsRevision(ctx context.Context, id int64) (store.StandardsRevision, error) {
	r, err := s.source.GetStandardsRevision(ctx, id)
	if s.revision != nil {
		s.revision(&r)
	}
	return r, err
}
func (s bookReads) GetStandardsProposal(ctx context.Context, id string, version int64) (store.StandardsProposalVersion, error) {
	p, err := s.source.GetStandardsProposal(ctx, id, version)
	if s.proposal != nil {
		s.proposal(&p)
	}
	return p, err
}

func TestReadOnlyBookCountsFiltersAndVersion(t *testing.T) {
	svc, _, db := publicationServices(t)
	rule := func(id, cat string, roles ...string) standards.Operation {
		return standards.Operation{Kind: "standard.add", Standard: &standards.Standard{ID: id, CategoryID: cat, Rule: "MUST " + id, Description: "Description.", Evidence: "Evidence.", Scope: "All repositories.", Check: "manual", AppliesTo: roles}}
	}
	p, err := svc.Create(t.Context(), "book", 0, []standards.Operation{
		{Kind: "category.add", Category: &standards.Category{ID: "code", Name: "Code"}},
		{Kind: "category.add", Category: &standards.Category{ID: "tests", Name: "Tests"}},
		{Kind: "category.add", Category: &standards.Category{ID: "empty", Name: "Empty"}},
		rule("both", "code", "story-worker", "plan-technical"),
		rule("worker", "tests", "story-worker"),
		rule("reviewer", "tests", "validate-code"),
	})
	if err != nil {
		t.Fatal(err)
	}
	approvePublication(t, svc, p)
	revision, err := svc.Publish(t.Context(), p.ProposalID, p.Version, p.ChangeDigest)
	if err != nil {
		t.Fatal(err)
	}
	book, err := standards.ReadBook(t.Context(), bookReads{source: db}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if book.RevisionID != revision.ID || book.Digest != revision.Digest {
		t.Fatal("book identity differs")
	}
	counts, err := book.RoleCounts([]string{"code", "code"})
	if err != nil {
		t.Fatal(err)
	}
	wantWhole := map[string]int{"story-worker": 2, "plan-technical": 1, "validate-code": 1}
	wantSelected := map[string]int{"story-worker": 1, "plan-technical": 1, "validate-code": 0}
	if !reflect.DeepEqual(counts.WholeBook, wantWhole) || !reflect.DeepEqual(counts.SelectedCategories, wantSelected) || !reflect.DeepEqual(counts.SelectedCategoryIDs, []string{"code"}) {
		t.Fatalf("counts %+v", counts)
	}
	empty, err := book.RoleCounts([]string{"empty"})
	if err != nil {
		t.Fatal(err)
	}
	for role, count := range empty.SelectedCategories {
		if count != 0 {
			t.Fatalf("empty category %s=%d", role, count)
		}
	}
	all, err := book.RoleCounts(nil)
	if err != nil || !reflect.DeepEqual(all.SelectedCategories, wantWhole) {
		t.Fatalf("unfiltered counts %+v %v", all, err)
	}
	filtered, err := book.Project("story-worker", []string{"code"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Standards) != 1 || filtered.StandardIDs[0] != "both" || !strings.Contains(filtered.Text, "MUST both") {
		t.Fatalf("filter %+v", filtered)
	}
	filtered.Standards[0].AppliesTo[0] = "mutated"
	again, err := book.Project("story-worker", []string{"code"})
	if err != nil || reflect.DeepEqual(filtered.Standards, again.Standards) {
		t.Fatal("projection mutated retained snapshot")
	}
	blank, err := book.Project("story-worker", []string{"empty"})
	if err != nil || len(blank.Standards) != 0 || len(blank.Categories) != 1 {
		t.Fatalf("empty projection %+v %v", blank, err)
	}
	if _, err = book.Project("unknown", nil); !errors.Is(err, standards.ErrInvalid) {
		t.Fatalf("unknown role accepted: %v", err)
	}
	if _, err = book.RoleCounts([]string{"missing"}); !errors.Is(err, standards.ErrUnknownCategory) {
		t.Fatalf("unknown category accepted: %v", err)
	}
	p, err = svc.Create(t.Context(), "next-book", revision.ID, []standards.Operation{rule("later", "code", "story-worker")})
	if err != nil {
		t.Fatal(err)
	}
	approvePublication(t, svc, p)
	if _, err = svc.Publish(t.Context(), p.ProposalID, p.Version, p.ChangeDigest); err != nil {
		t.Fatal(err)
	}
	current, err := standards.ReadBook(t.Context(), db, 0)
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := standards.ReadBook(t.Context(), db, book.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if current.RevisionID == book.RevisionID || pinned.Digest != book.Digest {
		t.Fatal("published update changed pinned identity")
	}
	pinnedCounts, err := pinned.RoleCounts(nil)
	if err != nil || !reflect.DeepEqual(pinnedCounts.WholeBook, wantWhole) {
		t.Fatalf("pinned counts %+v %v", pinnedCounts, err)
	}
	if _, err = standards.ReadBook(t.Context(), db, current.RevisionID+100); err == nil {
		t.Fatal("missing version silently substituted")
	}
}

func TestReadOnlyBookRefusesCorruptPublishedSources(t *testing.T) {
	runtime, db := runtimeFixture(t)
	status, err := runtime.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		source bookReads
	}{
		{"snapshot digest", bookReads{source: db, revision: func(r *store.StandardsRevision) { r.Digest = "changed" }}},
		{"publication", bookReads{source: db, revision: func(r *store.StandardsRevision) { r.Publication = nil }}},
		{"schema", bookReads{source: db, revision: func(r *store.StandardsRevision) { r.SchemaVersion = 2 }}},
		{"proposal operations", bookReads{source: db, proposal: func(p *store.StandardsProposalVersion) { p.Operations = nil }}},
		{"proposal publication link", bookReads{source: db, proposal: func(p *store.StandardsProposalVersion) { p.ResultDigest = "different" }}},
		{"revision identity", bookReads{source: db, revision: func(r *store.StandardsRevision) { r.ID++ }}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := standards.ReadBook(t.Context(), tc.source, status.Binding.RevisionID); !errors.Is(err, store.ErrStandardsCorrupt) {
				t.Fatalf("corrupt source accepted: %v", err)
			}
		})
	}
}

func TestReadOnlyBookRefusesOversizedSnapshot(t *testing.T) {
	svc, _, db := publicationServices(t)
	p, err := svc.Create(t.Context(), "large", 0, []standards.Operation{
		{Kind: "category.add", Category: &standards.Category{ID: "large", Name: "Large"}},
		{Kind: "standard.add", Standard: &standards.Standard{ID: "large", CategoryID: "large", Rule: "MUST Large", Description: strings.Repeat("x", 1<<20), Evidence: "Evidence.", Scope: "All repositories.", Check: "manual", AppliesTo: []string{"story-worker"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	approvePublication(t, svc, p)
	if _, err = svc.Publish(t.Context(), p.ProposalID, p.Version, p.ChangeDigest); err != nil {
		t.Fatal(err)
	}
	if _, err = standards.ReadBook(t.Context(), db, 0); !errors.Is(err, standards.ErrBookTooLarge) {
		t.Fatalf("oversized book accepted: %v", err)
	}
	// The general published-book contract remains available to existing callers.
	if _, err = svc.Revision(t.Context(), 0); err != nil {
		t.Fatalf("existing caller inherited remote limit: %v", err)
	}
}
