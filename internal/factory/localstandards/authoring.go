package localstandards

import (
	"context"
	"time"

	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/factorylib"
	"github.com/cloudvane/litefactory/internal/projectread"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

// AuthorAttempt is retained authoring evidence, without persistence access.
type AuthorAttempt struct {
	ID             string    `json:"id"`
	SessionID      string    `json:"session_id"`
	InputVersion   int64     `json:"input_version"`
	Token          string    `json:"token"`
	LeaseUntil     time.Time `json:"lease_until"`
	InputDigest    string    `json:"input_digest"`
	RoleVersion    string    `json:"role_version"`
	Provider       string    `json:"provider"`
	Model          string    `json:"model"`
	UsageReference string    `json:"usage_reference"`
	Outcome        string    `json:"outcome"`
	Diagnostic     string    `json:"diagnostic"`
	ResultVersion  int64     `json:"result_version"`
	CreatedAt      time.Time `json:"created_at"`
	FinishedAt     time.Time `json:"finished_at,omitempty"`
}

// AuthorSession is the bounded retained author session view.
type AuthorSession struct {
	ID       string    `json:"id"`
	Version  int64     `json:"version"`
	Mode     string    `json:"mode"`
	TargetID string    `json:"target_id"`
	Status   string    `json:"status"`
	Proposal *Proposal `json:"proposal,omitempty"`
	standards.AuthoringState
	Attempts []AuthorAttempt `json:"attempts,omitempty"`
}

func authorSession(v standards.AuthoringSession) AuthorSession {
	out := AuthorSession{ID: v.ID, Version: v.Version, Mode: v.Mode, TargetID: v.TargetID, Status: v.Status, AuthoringState: v.AuthoringState}
	if v.Proposal != nil {
		p := Proposal(*v.Proposal)
		out.Proposal = &p
	}
	if v.Attempts != nil {
		out.Attempts = make([]AuthorAttempt, len(v.Attempts))
		for i, a := range v.Attempts {
			out.Attempts[i] = AuthorAttempt(a)
		}
	}
	return out
}

// AuthorDependencies supplies local provider and instruction composition only.
type AuthorDependencies struct {
	Instructions factorylib.Source
	Runner       standards.AuthoringRunner
	Projects     interface {
		Pin(context.Context, int64) (projectread.Project, error)
	}
}

// Authoring owns local author operations and their registered project binding.
type Authoring struct {
	db      *store.DB
	service standards.Authoring
}

// NewAuthoring resolves local author authority before dependency composition.
func NewAuthoring(ctx context.Context, db *store.DB, mutation bool) (*Authoring, error) {
	owner, err := localOwner(ctx, db, mutation)
	if err != nil {
		return nil, err
	}
	return &Authoring{db: db, service: standards.Authoring{Store: db, Book: &standards.Service{Store: db, OwnerID: owner}}}, nil
}

// Configure supplies the trusted author execution dependencies.
func (s *Authoring) Configure(d AuthorDependencies) {
	s.service.Instructions = d.Instructions
	s.service.Projects = d.Projects
	s.service.Runner = d.Runner
}

// Start starts the explicitly requested local operation.
func (s *Authoring) Start(ctx context.Context, r contracts.StandardsAuthorStart) (AuthorSession, error) {
	var projectID int64
	if r.ProjectForge != "" {
		repo, err := s.db.GetRepoBySlug(ctx, r.ProjectForge, r.ProjectOwner, r.ProjectName)
		if err != nil {
			return AuthorSession{}, err
		}
		projectID = repo.ID
	}
	mode := "create"
	if r.TargetID != "" {
		mode = "edit"
	}
	v, err := s.service.Start(ctx, standards.AuthoringStart{ID: r.ID, Mode: mode, TargetID: r.TargetID, Intent: r.Intent, CategoryIDs: r.Categories, ProjectID: projectID})
	return authorSession(v), err
}

// List reads the existing bounded administrative listing.
func (s *Authoring) List(ctx context.Context, limit int) ([]AuthorSession, error) {
	rows, err := s.service.List(ctx, limit)
	if err != nil {
		return nil, err
	}
	var out []AuthorSession
	if rows != nil {
		out = make([]AuthorSession, len(rows))
		for i, v := range rows {
			out[i] = authorSession(v)
		}
	}
	return out, nil
}

// Show reads the requested saved version.
func (s *Authoring) Show(ctx context.Context, id string, version int64) (AuthorSession, error) {
	v, err := s.service.Show(ctx, id, version)
	return authorSession(v), err
}

// Answer admits the exact research answer while preserving lost-acknowledgment handling.
func (s *Authoring) Answer(ctx context.Context, id string, expected int64, answers []standards.AuthoringAnswer) (AuthorSession, error) {
	v, err := s.service.Answer(ctx, id, expected, answers)
	return authorSession(v), err
}

// Edit records a conditional proposal or draft revision.
func (s *Authoring) Edit(ctx context.Context, id string, expected int64, edit standards.AuthoringEdit) (AuthorSession, error) {
	v, err := s.service.Edit(ctx, id, expected, edit)
	return authorSession(v), err
}

// Continue continues the exact saved author version.
func (s *Authoring) Continue(ctx context.Context, id string, expected int64) (AuthorSession, error) {
	v, err := s.service.Continue(ctx, id, expected)
	return authorSession(v), err
}

// Validate checks the exact saved author version.
func (s *Authoring) Validate(ctx context.Context, id string, expected int64) (AuthorSession, error) {
	v, err := s.service.Validate(ctx, id, expected)
	return authorSession(v), err
}

// Complete creates the author draft proposal for separate approval.
func (s *Authoring) Complete(ctx context.Context, id string, expected int64) (Proposal, error) {
	v, err := s.service.Complete(ctx, id, expected)
	return Proposal(v), err
}

// Rebase rebases the author draft against the explicitly selected book revision.
func (s *Authoring) Rebase(ctx context.Context, id string, expected, revision int64) (AuthorSession, error) {
	v, err := s.service.Rebase(ctx, id, expected, revision)
	return authorSession(v), err
}
