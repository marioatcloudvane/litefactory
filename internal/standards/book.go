package standards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/cloudvane/litefactory/internal/store"
)

// PublishedStore is the immutable read boundary shared by book consumers. It
// deliberately has no authoring, run-binding or observation methods.
type PublishedStore interface {
	GetStandardsRevision(context.Context, int64) (store.StandardsRevision, error)
	GetStandardsProposal(context.Context, string, int64) (store.StandardsProposalVersion, error)
}

// ReadPublishedRevision verifies the snapshot and its retained publication and
// proposal together. A positive id never substitutes the current book.
func ReadPublishedRevision(ctx context.Context, source PublishedStore, id int64) (store.StandardsRevision, error) {
	r, err := source.GetStandardsRevision(ctx, id)
	if err != nil {
		return r, err
	}
	if id < 0 || (id > 0 && r.ID != id) {
		return r, fmt.Errorf("%w: revision identity mismatch", store.ErrStandardsCorrupt)
	}
	if err = VerifyRevision(r); err != nil {
		return r, err
	}
	p := r.Publication
	proposal, err := source.GetStandardsProposal(ctx, p.ProposalID, p.Version)
	if err != nil {
		return r, fmt.Errorf("%w: revision %d proposal: %w", store.ErrStandardsCorrupt, r.ID, err)
	}
	if proposal.SchemaVersion != 1 || changeDigest(proposal.BaseRevision, proposal.Operations) != proposal.ChangeDigest || proposal.ProposalID != p.ProposalID || proposal.Version != p.Version || proposal.BaseRevision != p.BaseRevision || proposal.ChangeDigest != p.ChangeDigest || proposal.ResultDigest != p.ResultDigest {
		return r, fmt.Errorf("%w: revision %d proposal binding/digest/schema mismatch", store.ErrStandardsCorrupt, r.ID)
	}
	r.Snapshot = canonical(r.Snapshot)
	return r, nil
}

// ErrBookTooLarge requires a narrower evidence source rather than disclosing a
// clipped book whose omitted rules could change the applicability counts.
var ErrBookTooLarge = errors.New("standards: published book exceeds 1 MiB evidence limit")

// Book is a verified immutable published version. The snapshot remains private
// so filtering one read cannot mutate a later read or its applicability counts.
type Book struct {
	RevisionID int64  `json:"book_revision"`
	Digest     string `json:"book_digest"`
	snapshot   Snapshot
}

// boundedPublishedReader is optional so existing immutable fake/caller contracts
// stay compatible while the actual database checks bounds before allocation.
type boundedPublishedReader interface {
	GetStandardsRevisionBounded(context.Context, int64, int) (store.StandardsRevision, error)
	GetStandardsProposalBounded(context.Context, string, int64, int) (store.StandardsProposalVersion, error)
}
type boundedBookSource struct{ source boundedPublishedReader }

func (s boundedBookSource) GetStandardsRevision(ctx context.Context, id int64) (store.StandardsRevision, error) {
	return s.source.GetStandardsRevisionBounded(ctx, id, 1<<20)
}
func (s boundedBookSource) GetStandardsProposal(ctx context.Context, id string, version int64) (store.StandardsProposalVersion, error) {
	return s.source.GetStandardsProposalBounded(ctx, id, version, 1<<20)
}

// ReadBook reads current (id zero) or one exact retained published version.
// It creates no runtime binding, selection or observation.
func ReadBook(ctx context.Context, source PublishedStore, id int64) (Book, error) {
	if bounded, ok := source.(boundedPublishedReader); ok {
		source = boundedBookSource{bounded}
	}
	r, err := ReadPublishedRevision(ctx, source, id)
	if errors.Is(err, store.ErrStandardsReadTooLarge) {
		return Book{}, ErrBookTooLarge
	}
	if err != nil {
		return Book{}, err
	}
	raw, err := json.Marshal(r.Snapshot)
	if err != nil {
		return Book{}, err
	}
	if len(raw) > 1<<20 {
		return Book{}, ErrBookTooLarge
	}
	return Book{RevisionID: r.ID, Digest: r.Digest, snapshot: r.Snapshot}, nil
}

// Roles returns the published applicability roster, independent of filters.
func (b Book) Roles() []string {
	roles := []string{}
	for role := range BookLoad(b.snapshot).Roles {
		roles = append(roles, role)
	}
	slices.Sort(roles)
	return roles
}

// BookProjection contains the selected rules and their common book identity.
type BookProjection struct {
	RevisionID  int64      `json:"book_revision"`
	Digest      string     `json:"book_digest"`
	Role        string     `json:"role,omitempty"`
	Categories  []Category `json:"categories"`
	Standards   []Standard `json:"standards"`
	StandardIDs []string   `json:"standard_ids"`
	Text        string     `json:"text"`
}

// Project filters by the published roster and known category IDs. A known
// category without rules is a valid empty selection.
func (b Book) Project(role string, categories []string) (BookProjection, error) {
	out := BookProjection{RevisionID: b.RevisionID, Digest: b.Digest, Role: role, Categories: []Category{}, Standards: []Standard{}, StandardIDs: []string{}}
	chosen, err := b.categories(categories)
	if err != nil {
		return out, err
	}
	if role != "" && !slices.Contains(b.Roles(), role) {
		return out, fmt.Errorf("%w: unknown published role", ErrInvalid)
	}
	for _, c := range b.snapshot.Categories {
		if len(chosen) == 0 || slices.Contains(chosen, c.ID) {
			out.Categories = append(out.Categories, c)
		}
	}
	out.Standards = filterStandards(b.snapshot, role, chosen)
	for _, s := range out.Standards {
		out.StandardIDs = append(out.StandardIDs, s.ID)
	}
	out.Text = Render(out.Categories, out.Standards)
	return out, nil
}

// BookRoleCounts labels both applicability bases. Each standard counts once
// for every role in AppliesTo; adding role counts is not a unique-rule count.
type BookRoleCounts struct {
	RevisionID          int64          `json:"book_revision"`
	Digest              string         `json:"book_digest"`
	SelectedCategoryIDs []string       `json:"selected_category_ids"`
	SelectedCategories  map[string]int `json:"selected_categories"`
	WholeBook           map[string]int `json:"whole_book"`
}

// RoleCounts compares selected categories and the whole immutable book.
func (b Book) RoleCounts(categories []string) (BookRoleCounts, error) {
	out := BookRoleCounts{RevisionID: b.RevisionID, Digest: b.Digest, SelectedCategories: map[string]int{}, WholeBook: BookLoad(b.snapshot).Roles}
	chosen, err := b.categories(categories)
	if err != nil {
		return out, err
	}
	out.SelectedCategoryIDs = chosen
	if len(chosen) == 0 {
		for _, c := range b.snapshot.Categories {
			out.SelectedCategoryIDs = append(out.SelectedCategoryIDs, c.ID)
		}
	}
	selected := BookLoad(Snapshot{Standards: filterStandards(b.snapshot, "", chosen)}).Roles
	for role := range out.WholeBook {
		out.SelectedCategories[role] = selected[role]
	}
	return out, nil
}

func (b Book) categories(categories []string) ([]string, error) {
	chosen := slices.Compact(slices.Sorted(slices.Values(categories)))
	for _, id := range chosen {
		if !slices.ContainsFunc(b.snapshot.Categories, func(c Category) bool { return c.ID == id }) {
			return nil, fmt.Errorf("%w: %s", ErrUnknownCategory, id)
		}
	}
	return chosen, nil
}

// filterStandards is shared with retained runtime consumers. Canonical copies
// keep nested role/policy slices from escaping the immutable source snapshot.
func filterStandards(snap Snapshot, role string, categories []string) []Standard {
	out := []Standard{}
	for _, s := range snap.Standards {
		if (role == "" || slices.Contains(s.AppliesTo, role)) && (len(categories) == 0 || slices.Contains(categories, s.CategoryID)) {
			out = append(out, s)
		}
	}
	return canonical(Snapshot{Standards: out}).Standards
}
