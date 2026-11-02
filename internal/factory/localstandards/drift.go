package localstandards

import (
	"context"
	"errors"
	"log/slog"

	"github.com/cloudvane/litefactory/internal/drift"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

func driftAnalysis(a store.DriftAnalysis) DriftAnalysis {
	out := DriftAnalysis{ID: a.ID, RunID: a.RunID, Repository: a.Repository, RequestKey: a.RequestKey, RequestDigest: a.RequestDigest, Scope: a.Scope, Pinned: a.Pinned, Limits: DriftLimits(a.Limits), Usage: DriftUsage(a.Usage), Generation: a.Generation, Version: a.Version, Execution: a.Execution, Disposition: a.Disposition, Coverage: a.Coverage, Standards: a.Standards, Provenance: a.Provenance, Report: a.Report, Reason: a.Reason, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
	if a.Offers != nil {
		out.Offers = make([]DriftOffer, len(a.Offers))
		for i, o := range a.Offers {
			out.Offers[i] = DriftOffer(o)
		}
	}
	if a.ProposalLinks != nil {
		out.ProposalLinks = make([]DriftProposalLink, len(a.ProposalLinks))
		for i, p := range a.ProposalLinks {
			out.ProposalLinks[i] = DriftProposalLink(p)
		}
	}
	return out
}

// DriftInspection is the bounded retained drift inspection view.
type DriftInspection struct {
	Analysis  DriftAnalysis `json:"analysis"`
	Standards RunInspection `json:"standards"`
}

// Drift owns retained standalone analysis operations and their existing evidence fences.
type Drift struct {
	sourceRepo *store.Repo
	db         *store.DB
	service    drift.Service
}

// NewDrift binds standalone drift operations to trusted storage.
func NewDrift(db *store.DB) *Drift { return &Drift{db: db, service: drift.Service{Store: db}} }

// List reads the existing bounded administrative listing.
func (s *Drift) List(ctx context.Context, repository string, offset, limit int) ([]DriftAnalysis, error) {
	rows, err := s.db.ListDriftAnalyses(ctx, repository, offset, limit)
	if err != nil {
		return nil, err
	}
	var out []DriftAnalysis
	if rows != nil {
		out = make([]DriftAnalysis, len(rows))
		for i, r := range rows {
			out[i] = driftAnalysis(r)
		}
	}
	return out, nil
}

// Inspect reads the retained evidence for the requested identity.
func (s *Drift) Inspect(ctx context.Context, id string) (DriftInspection, error) {
	v, err := s.service.Inspect(ctx, id)
	return DriftInspection{Analysis: driftAnalysis(v.Analysis), Standards: InspectRuntime(v.Standards)}, err
}

// ExpireEvidence expires retained evidence under the existing analysis fences.
func (s *Drift) ExpireEvidence(ctx context.Context, id string) error {
	return s.service.ExpireEvidence(ctx, id)
}

// Evidence reads one item from the retained analysis.
func (s *Drift) Evidence(ctx context.Context, id, evidenceID string) (DriftEvidence, error) {
	if _, err := s.db.GetDriftAnalysis(ctx, id); err != nil {
		return DriftEvidence{}, err
	}
	v, err := s.db.GetDriftEvidence(ctx, id, evidenceID)
	return DriftEvidence(v), err
}

// EvidencePage reads the bounded retained evidence page.
func (s *Drift) EvidencePage(ctx context.Context, id string, offset, limit int) ([]DriftEvidence, error) {
	if _, err := s.db.GetDriftAnalysis(ctx, id); err != nil {
		return nil, err
	}
	rows, err := s.db.ListDriftEvidence(ctx, id, offset, limit)
	if err != nil {
		return nil, err
	}
	var out []DriftEvidence
	if rows != nil {
		out = make([]DriftEvidence, len(rows))
		for i, r := range rows {
			out[i] = DriftEvidence(r)
		}
	}
	return out, nil
}

// DriftSourceOptions contains provider/Git composition, without source-selection authority.
type DriftSourceOptions struct {
	Git             *git.Binary
	Redactor        *redact.Redactor
	Resolver        secrets.Resolver
	Logger          *slog.Logger
	LegacyReference secrets.Reference
	LegacyToken     secrets.Token
	GitHubAPIBase   string
	Discussion      drift.DiscussionReader
}

// RegisteredRepositoryRequired preserves the local registration precondition.
type RegisteredRepositoryRequired struct{ Err error }

// Error returns the existing refusal text.
func (e *RegisteredRepositoryRequired) Error() string { return e.Err.Error() }

// Unwrap preserves the underlying lookup error for local exit mapping.
func (e *RegisteredRepositoryRequired) Unwrap() error { return e.Err }

// SelectRepository pins the registered source repository before provider composition.
func (s *Drift) SelectRepository(ctx context.Context, owner, name string) error {
	repo, err := s.db.GetRepoBySlug(ctx, store.ForgeGitHub, owner, name)
	if err != nil {
		return &RegisteredRepositoryRequired{Err: err}
	}
	s.sourceRepo = &repo
	return nil
}

// ConfigureSource composes source readers under the pinned repository and retained connection.
func (s *Drift) ConfigureSource(ctx context.Context, owner, name string, o DriftSourceOptions) error {
	if s.sourceRepo == nil || s.sourceRepo.Owner != owner || s.sourceRepo.Name != name {
		if err := s.SelectRepository(ctx, owner, name); err != nil {
			return err
		}
	}
	repo := *s.sourceRepo
	source := &drift.Acquirer{Store: s.db, Git: o.Git, ClonePath: repo.Path, Repo: github.Repo{Owner: owner, Name: name}, Redactor: o.Redactor}
	if o.Discussion != nil {
		source.Discussion = o.Discussion
	} else {
		factory := projectconnection.NewFactory(projectconnection.Options{DB: s.db, Resolver: o.Resolver, Logger: o.Logger, Redactor: o.Redactor, LegacyReference: o.LegacyReference, LegacyToken: o.LegacyToken})
		if version, e := s.db.CurrentConnection(ctx, repo.ID, store.TicketConnection); e == nil {
			if clients, e := factory.Clients(ctx, version, 0); e == nil {
				source.Discussion = clients.Code
			}
		} else if errors.Is(e, store.ErrNotFound) && !o.LegacyToken.Empty() {
			client, e := github.New(github.Options{Token: o.LegacyToken, APIBase: o.GitHubAPIBase, Redactor: o.Redactor, Logger: o.Logger})
			if e == nil {
				source.Discussion = client
			}
		}
	}
	s.service.Acquisition = source
	return nil
}

// ConfigureRunner supplies the configured isolated runner or existing launch refusal.
func (s *Drift) ConfigureRunner(runner drift.Runner, refusal string) {
	s.service.Runner = runner
	s.service.LaunchRefusal = refusal
}

// OnCreated registers the local rendering callback for a newly retained analysis.
func (s *Drift) OnCreated(notify func(DriftAnalysis) error) {
	s.service.OnCreated = func(a store.DriftAnalysis) error { return notify(driftAnalysis(a)) }
}

// Start starts the explicitly requested local operation.
func (s *Drift) Start(ctx context.Context, r drift.Request) (DriftAnalysis, error) {
	v, err := s.service.Start(ctx, r)
	return driftAnalysis(v), err
}
