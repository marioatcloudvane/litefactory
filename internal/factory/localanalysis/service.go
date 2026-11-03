// Package localanalysis owns local log-analysis admission and recovery.
package localanalysis

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/loganalysis"
	"github.com/cloudvane/litefactory/internal/logevidence"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

// AnalysisView preserves the existing local analysis listing fields.
type AnalysisView struct {
	ID            string          `json:"id"`
	StartKey      string          `json:"start_key,omitempty"`
	RequestDigest string          `json:"request_digest"`
	Scope         json.RawMessage `json:"scope"`
	Limits        json.RawMessage `json:"limits"`
	GrantDigest   string          `json:"grant_digest"`
	Provenance    json.RawMessage `json:"provenance"`
	Generation    int64           `json:"generation"`
	State         string          `json:"state"`
	Disposition   string          `json:"disposition"`
	Calls         int             `json:"calls"`
	OutboundBytes int             `json:"outbound_bytes"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	Reason        string          `json:"reason,omitempty"`
}

// Service owns local analysis admission, inspection and recovery.
type Service struct {
	db       *store.DB
	analysis *loganalysis.Service
}

// New binds local analysis operations to trusted domain and storage dependencies.
func New(db *store.DB, analysis *loganalysis.Service) *Service {
	return &Service{db: db, analysis: analysis}
}

// List reads a bounded page of retained analysis summaries.
func (s *Service) List(ctx context.Context, after string, limit int) ([]AnalysisView, error) {
	rows, err := s.db.ListLogAnalyses(ctx, after, limit)
	var views []AnalysisView
	if rows != nil {
		views = make([]AnalysisView, len(rows))
		for i, row := range rows {
			views[i] = AnalysisView(row)
		}
	}
	return views, err
}

// Categories reads the immutable categories captured for this analysis.
func (s *Service) Categories(ctx context.Context, id string) []standards.Category {
	binding, err := s.db.GetAnalystStandardsBinding(ctx, id)
	if err != nil || len(binding.SnapshotBytes) == 0 {
		return nil
	}
	var snapshot standards.Snapshot
	if json.Unmarshal(binding.SnapshotBytes, &snapshot) != nil {
		return nil
	}
	return snapshot.Categories
}

// Scope reads the retained request and evidence selection for this analysis.
func (s *Service) Scope(ctx context.Context, id string) (loganalysis.Scope, error) {
	invocation, err := s.db.GetLogAnalysis(ctx, id)
	if err != nil {
		return loganalysis.Scope{}, err
	}
	var scope loganalysis.Scope
	err = json.Unmarshal(invocation.Scope, &scope)
	return scope, err
}

// Recover fences only an expired retained invocation, without relaunching it.
func (s *Service) Recover(ctx context.Context, id string) (Inspection, error) {
	invocation, err := s.db.GetLogAnalysis(ctx, id)
	if err != nil {
		return Inspection{}, err
	}
	var limits loganalysis.Limits
	if err = json.Unmarshal(invocation.Limits, &limits); err != nil {
		return Inspection{}, err
	}
	if invocation.State == "running" && time.Now().Before(invocation.CreatedAt.Add(time.Duration(limits.Seconds)*time.Second)) {
		return Inspection{}, errors.New("analysis deadline has not expired; recovery cannot fence an active invocation")
	}
	if err = s.db.InterruptLogAnalysis(ctx, invocation.ID, invocation.Generation, "incomplete", "orphaned invocation interrupted; no model relaunch or budget reset"); err != nil {
		return Inspection{}, err
	}
	return s.Inspect(ctx, invocation.ID, 0, 20)
}

// RepositoryID resolves the registered repository selected by the local operator.
func (s *Service) RepositoryID(ctx context.Context, forge, owner, name string) (int64, error) {
	repository, err := s.db.GetRepoBySlug(ctx, forge, owner, name)
	return repository.ID, err
}

// Inspect returns bounded retained analysis evidence.
func (s *Service) Inspect(ctx context.Context, id string, after int64, limit int) (Inspection, error) {
	view, err := s.analysis.Inspect(ctx, id, after, limit)
	return inspection(view), err
}

// Start admits the existing bounded standalone analysis operation.
func (s *Service) Start(ctx context.Context, request loganalysis.Request) (AnalysisView, error) {
	view, err := s.analysis.Start(ctx, request)
	return AnalysisView(view), err
}

// FileFindings retries only the existing retained filing operation.
func (s *Service) FileFindings(ctx context.Context, id string) error {
	return s.analysis.FileFindings(ctx, id)
}

// Reread applies existing retained citation limits and authority.
func (s *Service) Reread(ctx context.Context, id string, c logevidence.Citation) (logevidence.Result, error) {
	return s.analysis.Reread(ctx, id, c)
}
