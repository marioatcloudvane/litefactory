// Package standards validates and publishes exact, locally reviewed book changes.
package standards

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
)

// Category is a stable category in a book snapshot.
type Category = store.StandardsCategory

// Standard contains Markdown guidance and category memberships.
type Standard = store.StandardsStandard

// Snapshot is the complete contents of a retained book revision.
type Snapshot = store.StandardsSnapshot

// Operation is one explicit category or standard change.
type Operation = store.StandardsOperation

// ErrInvalid identifies structural or referential input errors.
var ErrInvalid = errors.New("standards: invalid proposal")

// ErrOwnerRequired refuses approval and publication without trusted owner identity.
var ErrOwnerRequired = errors.New("standards: initialized local owner required")

// Store is the persistence boundary; transitions recheck all conditional state.
type Store interface {
	StandardsHead(context.Context) (int64, error)
	GetStandardsRevision(context.Context, int64) (store.StandardsRevision, error)
	ListStandardsRevisions(context.Context) ([]store.StandardsRevision, error)
	GetStandardsProposal(context.Context, string, int64) (store.StandardsProposalVersion, error)
	ListStandardsProposals(context.Context) ([]store.StandardsProposalVersion, error)
	SaveStandardsProposal(context.Context, store.StandardsProposalVersion, int64) (store.StandardsProposalVersion, error)
	GetStandardsApproval(context.Context, string, int64) (store.StandardsApproval, error)
	ApproveStandardsProposal(context.Context, store.StandardsApproval) (store.StandardsApproval, error)
	PublishStandardsProposal(context.Context, store.StandardsProposalVersion, Snapshot, string) (store.StandardsRevision, error)
}

// Service owns validation, exact review, and trusted owner transitions.
type Service struct {
	Store   Store
	OwnerID int64
}

// Change shows the complete before and after values of an entity.
type Change struct {
	Entity string `json:"entity"`
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}

// Review binds the retained proposal to its complete resulting changes.
type Review struct {
	Proposal store.StandardsProposalVersion `json:"proposal"`
	Before   Snapshot                       `json:"before"`
	After    Snapshot                       `json:"after"`
	Changes  []Change                       `json:"changes"`
}

// Problem identifies an invalid field or entity.
type Problem struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationError collects actionable structural and referential problems.
type ValidationError struct {
	Problems []Problem `json:"problems"`
}

func (e *ValidationError) Error() string {
	var parts []string
	for _, p := range e.Problems {
		parts = append(parts, p.Field+": "+p.Message)
	}
	return ErrInvalid.Error() + ": " + strings.Join(parts, "; ")
}
func (e *ValidationError) Unwrap() error { return ErrInvalid }

func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func canonical(in Snapshot) Snapshot {
	out := Snapshot{Categories: append([]Category{}, in.Categories...), Standards: append([]Standard{}, in.Standards...)}
	for i := range out.Standards {
		out.Standards[i].CategoryIDs = append([]string{}, out.Standards[i].CategoryIDs...)
		sort.Strings(out.Standards[i].CategoryIDs)
	}
	sort.Slice(out.Categories, func(i, j int) bool { return out.Categories[i].ID < out.Categories[j].ID })
	sort.Slice(out.Standards, func(i, j int) bool { return out.Standards[i].ID < out.Standards[j].ID })
	return out
}
func snapshotDigest(in Snapshot) string {
	return digest(struct {
		Schema   int      `json:"schema"`
		Snapshot Snapshot `json:"snapshot"`
	}{1, canonical(in)})
}
func canonicalOps(ops []Operation) []Operation {
	out := append([]Operation{}, ops...)
	for i := range out {
		if out[i].Category != nil {
			c := *out[i].Category
			out[i].Category = &c
		}
		if out[i].Standard != nil {
			s := *out[i].Standard
			s.CategoryIDs = append([]string{}, s.CategoryIDs...)
			sort.Strings(s.CategoryIDs)
			out[i].Standard = &s
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, _ := json.Marshal(out[i])
		b, _ := json.Marshal(out[j])
		return string(a) < string(b)
	})
	return out
}
func changeDigest(base int64, ops []Operation) string {
	return digest(struct {
		Schema     int         `json:"schema"`
		Base       int64       `json:"base_revision"`
		Operations []Operation `json:"operations"`
	}{1, base, canonicalOps(ops)})
}

func (s *Service) base(ctx context.Context, id int64) (Snapshot, error) {
	if id == 0 {
		return canonical(Snapshot{}), nil
	}
	r, err := s.Revision(ctx, id)
	return r.Snapshot, err
}

// Create retains the first immutable version of a proposal.
func (s *Service) Create(ctx context.Context, id string, base int64, ops []Operation) (store.StandardsProposalVersion, error) {
	return s.save(ctx, id, 0, base, ops)
}

// Edit retains a new version conditional on the expected current version.
func (s *Service) Edit(ctx context.Context, id string, expected, base int64, ops []Operation) (store.StandardsProposalVersion, error) {
	if expected < 1 {
		return store.StandardsProposalVersion{}, fmt.Errorf("%w: expected_version must be positive", ErrInvalid)
	}
	return s.save(ctx, id, expected, base, ops)
}
func (s *Service) save(ctx context.Context, id string, expected, base int64, ops []Operation) (store.StandardsProposalVersion, error) {
	if strings.TrimSpace(id) == "" || base < 0 {
		return store.StandardsProposalVersion{}, fmt.Errorf("%w: proposal id is required and base_revision cannot be negative", ErrInvalid)
	}
	before, err := s.base(ctx, base)
	if err != nil {
		return store.StandardsProposalVersion{}, err
	}
	after, err := apply(before, ops)
	if err != nil {
		return store.StandardsProposalVersion{}, err
	}
	author := "local-cli"
	if s.OwnerID > 0 {
		author = strconv.FormatInt(s.OwnerID, 10)
	}
	p := store.StandardsProposalVersion{ProposalID: id, Version: expected + 1, BaseRevision: base, SchemaVersion: 1, Operations: canonicalOps(ops), ChangeDigest: changeDigest(base, ops), ResultDigest: snapshotDigest(after), Author: author}
	return s.Store.SaveStandardsProposal(ctx, p, expected)
}

// Proposal reads and verifies an exact version, or the current version when zero.
func (s *Service) Proposal(ctx context.Context, id string, version int64) (store.StandardsProposalVersion, error) {
	p, err := s.Store.GetStandardsProposal(ctx, id, version)
	if err != nil {
		return p, err
	}
	_, err = s.review(ctx, p)
	return p, err
}

// List returns verified current proposal versions.
func (s *Service) List(ctx context.Context) ([]store.StandardsProposalVersion, error) {
	ps, err := s.Store.ListStandardsProposals(ctx)
	if err != nil {
		return nil, err
	}
	var failures []error
	for _, p := range ps {
		if _, err = s.review(ctx, p); err != nil {
			failures = append(failures, fmt.Errorf("proposal %q version %d: %w", p.ProposalID, p.Version, err))
		}
	}
	if err := errors.Join(failures...); err != nil {
		return nil, err
	}
	return ps, nil
}

// Review verifies the exact proposal and displays its full changes.
func (s *Service) Review(ctx context.Context, id string, version int64) (Review, error) {
	p, err := s.Store.GetStandardsProposal(ctx, id, version)
	if err != nil {
		return Review{}, err
	}
	return s.review(ctx, p)
}

// Validate checks the same invariants used by review and publication.
func (s *Service) Validate(ctx context.Context, id string, version int64) error {
	_, err := s.Review(ctx, id, version)
	return err
}
func (s *Service) review(ctx context.Context, p store.StandardsProposalVersion) (Review, error) {
	if p.SchemaVersion != 1 || changeDigest(p.BaseRevision, p.Operations) != p.ChangeDigest {
		return Review{}, fmt.Errorf("%w: proposal %s version %d change digest/schema mismatch", store.ErrStandardsCorrupt, p.ProposalID, p.Version)
	}
	before, err := s.base(ctx, p.BaseRevision)
	if err != nil {
		return Review{}, err
	}
	after, err := apply(before, p.Operations)
	if err != nil {
		return Review{}, fmt.Errorf("%w: retained proposal invalid: %w", store.ErrStandardsCorrupt, err)
	}
	if snapshotDigest(after) != p.ResultDigest {
		return Review{}, fmt.Errorf("%w: proposal resulting digest mismatch", store.ErrStandardsCorrupt)
	}
	return Review{p, before, after, diff(before, after)}, nil
}
func (s *Service) exact(ctx context.Context, id string, version int64, d string) (Review, error) {
	if s.OwnerID < 1 {
		return Review{}, ErrOwnerRequired
	}
	if version < 1 || d == "" {
		return Review{}, fmt.Errorf("%w: exact version and reviewed change digest required", ErrInvalid)
	}
	r, err := s.Review(ctx, id, version)
	if err != nil {
		return r, err
	}
	if r.Proposal.ChangeDigest != d {
		return Review{}, fmt.Errorf("%w: reviewed digest differs", store.ErrStandardsConflict)
	}
	return r, nil
}

// Approve records local owner approval of an exact version and digest.
func (s *Service) Approve(ctx context.Context, id string, version int64, d string) (store.StandardsApproval, error) {
	r, err := s.exact(ctx, id, version, d)
	if err != nil {
		return store.StandardsApproval{}, err
	}
	p := r.Proposal
	return s.Store.ApproveStandardsProposal(ctx, store.StandardsApproval{ProposalID: id, Version: version, BaseRevision: p.BaseRevision, ChangeDigest: p.ChangeDigest, ResultDigest: p.ResultDigest, Owner: strconv.FormatInt(s.OwnerID, 10), Provenance: "local-cli"})
}

// Publish atomically publishes the exact approved proposal or recovers its retry.
func (s *Service) Publish(ctx context.Context, id string, version int64, d string) (store.StandardsRevision, error) {
	r, err := s.exact(ctx, id, version, d)
	if err != nil {
		return store.StandardsRevision{}, err
	}
	return s.Store.PublishStandardsProposal(ctx, r.Proposal, r.After, strconv.FormatInt(s.OwnerID, 10))
}

// Revision reads and verifies a complete immutable book revision.
func (s *Service) Revision(ctx context.Context, id int64) (store.StandardsRevision, error) {
	r, err := s.Store.GetStandardsRevision(ctx, id)
	if err != nil {
		return r, err
	}
	if r.SchemaVersion != 1 || snapshotDigest(r.Snapshot) != r.Digest {
		return r, fmt.Errorf("%w: revision %d digest/schema mismatch", store.ErrStandardsCorrupt, id)
	}
	if err = validateSnapshot(r.Snapshot); err != nil {
		return r, fmt.Errorf("%w: revision %d invalid: %w", store.ErrStandardsCorrupt, id, err)
	}
	if r.Publication == nil {
		return r, fmt.Errorf("%w: revision %d missing publication", store.ErrStandardsCorrupt, id)
	}
	proposal, err := s.Store.GetStandardsProposal(ctx, r.Publication.ProposalID, r.Publication.Version)
	if err != nil {
		return r, fmt.Errorf("%w: revision %d proposal: %w", store.ErrStandardsCorrupt, id, err)
	}
	if proposal.SchemaVersion != 1 || changeDigest(proposal.BaseRevision, proposal.Operations) != proposal.ChangeDigest {
		return r, fmt.Errorf("%w: revision %d proposal digest/schema mismatch", store.ErrStandardsCorrupt, id)
	}
	r.Snapshot = canonical(r.Snapshot)
	return r, nil
}

// Current resolves the published head and verifies its retained snapshot.
func (s *Service) Current(ctx context.Context) (store.StandardsRevision, error) {
	id, err := s.Store.StandardsHead(ctx)
	if err != nil {
		return store.StandardsRevision{}, err
	}
	if id == 0 {
		return store.StandardsRevision{}, store.ErrStandardsUnpublished
	}
	return s.Revision(ctx, id)
}

// History returns verified complete retained revisions.
func (s *Service) History(ctx context.Context) ([]store.StandardsRevision, error) {
	rs, err := s.Store.ListStandardsRevisions(ctx)
	if err != nil {
		return nil, err
	}
	var failures []error
	for i, r := range rs {
		rs[i], err = s.Revision(ctx, r.ID)
		if err != nil {
			failures = append(failures, fmt.Errorf("revision %d: %w", r.ID, err))
		}
	}
	if err := errors.Join(failures...); err != nil {
		return nil, err
	}
	return rs, nil
}

// Diff compares two retained revisions; zero represents the empty base.
func (s *Service) Diff(ctx context.Context, before, after int64) ([]Change, error) {
	b, err := s.base(ctx, before)
	if err != nil {
		return nil, err
	}
	a, err := s.base(ctx, after)
	if err != nil {
		return nil, err
	}
	return diff(b, a), nil
}
func diff(b, a Snapshot) []Change {
	out := []Change{}
	bc := map[string]Category{}
	ac := map[string]Category{}
	bs := map[string]Standard{}
	as := map[string]Standard{}
	for _, v := range b.Categories {
		bc[v.ID] = v
	}
	for _, v := range a.Categories {
		ac[v.ID] = v
	}
	for _, v := range b.Standards {
		bs[v.ID] = v
	}
	for _, v := range a.Standards {
		as[v.ID] = v
	}
	for id, v := range bc {
		w, ok := ac[id]
		if !ok {
			out = append(out, Change{"category", id, "remove", v, nil})
		} else if !reflect.DeepEqual(v, w) {
			out = append(out, Change{"category", id, "replace", v, w})
		}
	}
	for id, v := range ac {
		if _, ok := bc[id]; !ok {
			out = append(out, Change{"category", id, "add", nil, v})
		}
	}
	for id, v := range bs {
		w, ok := as[id]
		if !ok {
			out = append(out, Change{"standard", id, "remove", v, nil})
		} else if !reflect.DeepEqual(v, w) {
			out = append(out, Change{"standard", id, "replace", v, w})
		}
	}
	for id, v := range as {
		if _, ok := bs[id]; !ok {
			out = append(out, Change{"standard", id, "add", nil, v})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Entity != out[j].Entity {
			return out[i].Entity < out[j].Entity
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Inspection retains exact version approval and any immutable publication.
type Inspection struct {
	Proposal    store.StandardsProposalVersion `json:"proposal"`
	Approval    *store.StandardsApproval       `json:"approval"`
	Publication *store.StandardsPublication    `json:"publication"`
}

// Inspect returns an exact proposal with its retained approval and publication.
func (s *Service) Inspect(ctx context.Context, id string, version int64) (Inspection, error) {
	p, err := s.Proposal(ctx, id, version)
	if err != nil {
		return Inspection{}, err
	}
	out := Inspection{Proposal: p}
	a, err := s.Store.GetStandardsApproval(ctx, id, p.Version)
	if err == nil {
		if a.ProposalID != p.ProposalID || a.Version != p.Version || a.BaseRevision != p.BaseRevision || a.ChangeDigest != p.ChangeDigest || a.ResultDigest != p.ResultDigest || a.Owner == "" || a.Provenance != "local-cli" {
			return Inspection{}, fmt.Errorf("%w: approval binding mismatch", store.ErrStandardsCorrupt)
		}
		out.Approval = &a
	} else if !errors.Is(err, store.ErrStandardsApprovalRequired) {
		return Inspection{}, err
	}
	rs, err := s.History(ctx)
	if err != nil {
		return Inspection{}, err
	}
	for _, r := range rs {
		if r.Publication != nil && r.Publication.ProposalID == id && r.Publication.Version == p.Version {
			out.Publication = r.Publication
			approval := r.Publication.Approval
			out.Approval = &approval
			break
		}
	}
	return out, nil
}

// Prepare validates an unapproved proposal against an already verified retained
// snapshot, without writing or resolving a later published head.
func Prepare(id string, base int64, before Snapshot, ops []Operation, author string) (store.StandardsProposalVersion, error) {
	if strings.TrimSpace(id) == "" || base < 1 {
		return store.StandardsProposalVersion{}, ErrInvalid
	}
	if err := validateSnapshot(before); err != nil {
		return store.StandardsProposalVersion{}, err
	}
	after, err := apply(before, ops)
	if err != nil {
		return store.StandardsProposalVersion{}, err
	}
	return store.StandardsProposalVersion{ProposalID: id, Version: 1, BaseRevision: base, SchemaVersion: 1, Operations: canonicalOps(ops), ChangeDigest: changeDigest(base, ops), ResultDigest: snapshotDigest(after), Author: author}, nil
}
