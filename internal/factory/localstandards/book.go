package localstandards

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

// Proposal preserves administrative output without exposing persistence records
// or write capabilities.
type Proposal struct {
	ProposalID    string                `json:"proposal_id"`
	Version       int64                 `json:"version"`
	BaseRevision  int64                 `json:"base_revision"`
	SchemaVersion int                   `json:"schema_version"`
	Operations    []standards.Operation `json:"operations"`
	ChangeDigest  string                `json:"change_digest"`
	ResultDigest  string                `json:"result_digest"`
	Author        string                `json:"author"`
	CreatedAt     time.Time             `json:"created_at"`
}

// Approval is the bounded retained approval view.
type Approval struct {
	ProposalID   string    `json:"proposal_id"`
	Version      int64     `json:"version"`
	BaseRevision int64     `json:"base_revision"`
	ChangeDigest string    `json:"change_digest"`
	ResultDigest string    `json:"result_digest"`
	Owner        string    `json:"owner"`
	Provenance   string    `json:"provenance"`
	CreatedAt    time.Time `json:"created_at"`
}

// Publication is the bounded retained publication view.
type Publication struct {
	RevisionID   int64     `json:"revision_id"`
	ProposalID   string    `json:"proposal_id"`
	Version      int64     `json:"version"`
	BaseRevision int64     `json:"base_revision"`
	ChangeDigest string    `json:"change_digest"`
	ResultDigest string    `json:"result_digest"`
	Owner        string    `json:"owner"`
	Provenance   string    `json:"provenance"`
	CreatedAt    time.Time `json:"created_at"`
	Approval     Approval  `json:"approval"`
}

// Revision is the bounded retained revision view.
type Revision struct {
	ID             int64              `json:"id"`
	ParentRevision int64              `json:"parent_revision"`
	SchemaVersion  int                `json:"schema_version"`
	Digest         string             `json:"digest"`
	Snapshot       standards.Snapshot `json:"snapshot"`
	CreatedAt      time.Time          `json:"created_at"`
	Publication    *Publication       `json:"publication"`
}

// Review is the bounded retained review view.
type Review struct {
	Proposal Proposal           `json:"proposal"`
	Before   standards.Snapshot `json:"before"`
	After    standards.Snapshot `json:"after"`
	Changes  []standards.Change `json:"changes"`
	Rendered string             `json:"rendered"`
}

// Inspection is the bounded retained inspection view.
type Inspection struct {
	Proposal    Proposal     `json:"proposal"`
	Approval    *Approval    `json:"approval"`
	Publication *Publication `json:"publication"`
}

func publication(p *store.StandardsPublication) *Publication {
	if p == nil {
		return nil
	}
	return &Publication{RevisionID: p.RevisionID, ProposalID: p.ProposalID, Version: p.Version, BaseRevision: p.BaseRevision, ChangeDigest: p.ChangeDigest, ResultDigest: p.ResultDigest, Owner: p.Owner, Provenance: p.Provenance, CreatedAt: p.CreatedAt, Approval: Approval(p.Approval)}
}
func revision(r store.StandardsRevision) Revision {
	return Revision{ID: r.ID, ParentRevision: r.ParentRevision, SchemaVersion: r.SchemaVersion, Digest: r.Digest, Snapshot: r.Snapshot, CreatedAt: r.CreatedAt, Publication: publication(r.Publication)}
}

// Book owns local book, proposal and category operations over existing services.
type Book struct{ service standards.Service }

// NewBook resolves the local owner gate for book operations.
func NewBook(ctx context.Context, db *store.DB, requireOwner bool) (*Book, error) {
	owner, err := localOwner(ctx, db, requireOwner)
	if err != nil {
		return nil, err
	}
	return &Book{service: standards.Service{Store: db, OwnerID: owner}}, nil
}

// Create records a new immutable proposal version.
func (s *Book) Create(ctx context.Context, id string, base int64, ops []standards.Operation) (Proposal, error) {
	v, err := s.service.Create(ctx, id, base, ops)
	return Proposal(v), err
}

// Edit records a conditional proposal or draft revision.
func (s *Book) Edit(ctx context.Context, id string, expected, base int64, ops []standards.Operation) (Proposal, error) {
	v, err := s.service.Edit(ctx, id, expected, base, ops)
	return Proposal(v), err
}

// Proposal reads the exact retained proposal version.
func (s *Book) Proposal(ctx context.Context, id string, version int64) (Proposal, error) {
	v, err := s.service.Proposal(ctx, id, version)
	return Proposal(v), err
}

// List reads the existing bounded administrative listing.
func (s *Book) List(ctx context.Context) ([]Proposal, error) {
	rows, err := s.service.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []Proposal
	if rows != nil {
		out = make([]Proposal, len(rows))
	}
	for i, row := range rows {
		out[i] = Proposal(row)
	}
	return out, nil
}

// Review exposes the focused local operation.
func (s *Book) Review(ctx context.Context, id string, version int64) (Review, error) {
	v, err := s.service.Review(ctx, id, version)
	return Review{Proposal: Proposal(v.Proposal), Before: v.Before, After: v.After, Changes: v.Changes, Rendered: v.Rendered}, err
}

// Inspect reads the retained evidence for the requested identity.
func (s *Book) Inspect(ctx context.Context, id string, version int64) (Inspection, error) {
	v, err := s.service.Inspect(ctx, id, version)
	out := Inspection{Proposal: Proposal(v.Proposal), Publication: publication(v.Publication)}
	if v.Approval != nil {
		a := Approval(*v.Approval)
		out.Approval = &a
	}
	return out, err
}

// Approve records approval of the exact reviewed version and digest.
func (s *Book) Approve(ctx context.Context, id string, version int64, digest string) (Approval, error) {
	v, err := s.service.Approve(ctx, id, version, digest)
	return Approval(v), err
}

// Publish publishes the exact approved proposal through the existing domain service.
func (s *Book) Publish(ctx context.Context, id string, version int64, digest string) (Revision, error) {
	v, err := s.service.Publish(ctx, id, version, digest)
	return revision(v), err
}

// Current reads the current verified book revision.
func (s *Book) Current(ctx context.Context) (Revision, error) {
	v, err := s.service.Current(ctx)
	return revision(v), err
}

// History reads verified retained book revisions.
func (s *Book) History(ctx context.Context) ([]Revision, error) {
	rows, err := s.service.History(ctx)
	if err != nil {
		return nil, err
	}
	var out []Revision
	if rows != nil {
		out = make([]Revision, len(rows))
	}
	for i, r := range rows {
		out[i] = revision(r)
	}
	return out, nil
}

// Revision reads one verified retained book revision.
func (s *Book) Revision(ctx context.Context, id int64) (Revision, error) {
	v, err := s.service.Revision(ctx, id)
	return revision(v), err
}

// Diff compares two verified book revisions.
func (s *Book) Diff(ctx context.Context, before, after int64) ([]standards.Change, error) {
	return s.service.Diff(ctx, before, after)
}

// ChangeCategory amends the reviewed category operation and conditionally records the proposal.
func (s *Book) ChangeCategory(ctx context.Context, r contracts.StandardsCategoryChange) (Proposal, error) {
	p, err := s.service.Proposal(ctx, r.Proposal, r.Expected)
	if err != nil {
		return Proposal{}, err
	}
	op := standards.Operation{Kind: "category." + r.Action, ID: r.ID}
	if r.Action != "remove" {
		op.ID = ""
		op.Category = &standards.Category{ID: r.ID, Name: r.Name, Description: r.Description}
	}
	ops, err := AmendCategory(p.Operations, op, r.Action, r.ID)
	if err != nil {
		return Proposal{}, err
	}
	return s.Edit(ctx, r.Proposal, r.Expected, p.BaseRevision, ops)
}

// AmendCategory preserves pending category additions when replacing or removing their operation.
func AmendCategory(original []standards.Operation, op standards.Operation, action, categoryID string) ([]standards.Operation, error) {
	ops := append([]standards.Operation{}, original...)
	found := false
	for i, existing := range ops {
		id := existing.ID
		if existing.Category != nil {
			id = existing.Category.ID
		}
		if id != categoryID || (existing.Kind != "category.add" && existing.Kind != "category.replace" && existing.Kind != "category.remove") {
			continue
		}
		found = true
		if action == "add" {
			return nil, fmt.Errorf("%w: category %s already has an operation; use replace or edit the complete change set", standards.ErrInvalid, categoryID)
		}
		if action == "remove" && existing.Kind == "category.add" {
			ops = append(ops[:i:i], ops[i+1:]...)
		} else {
			if action == "replace" && existing.Kind == "category.add" {
				op.Kind = "category.add"
			}
			ops[i] = op
		}
		break
	}
	if !found {
		ops = append(ops, op)
	}
	return ops, nil
}
