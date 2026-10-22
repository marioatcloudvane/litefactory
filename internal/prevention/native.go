package prevention

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// NewRuntime wires only local capabilities. Source and provider failures are
// evaluated after Start has retained the invocation, so its ID stays inspectable.
func NewRuntime(ctx context.Context, db *store.DB, cfg *config.Config, paths config.Paths, logger *slog.Logger, resolver secrets.Resolver) (*Service, error) {
	if db == nil || cfg == nil {
		return nil, errors.New("initialized analysis runtime required")
	}
	redactor := redact.New(redact.Options{Logger: logger, Environ: os.Environ()})
	source := &configuredSource{db: db, resolver: resolver, redactor: redactor, logger: logger}
	kind := ""
	order := cfg.AgentOrder()
	if len(order) > 0 {
		kind = string(order[0])
	}
	model, _ := cfg.Tiers.ResolveModel(config.Kind(kind), config.TierFlagship)
	runner := &nativeRunner{paths: paths, model: model, provider: kind}
	if kind == "claude" && cfg.Agents.Claude != nil {
		r, err := harness.New(harness.Options{Paths: paths, Claude: *cfg.Agents.Claude, Model: model, Environ: os.Environ(), Logger: logger})
		if err == nil {
			runner.providerRunner = r
		} else {
			runner.refusal = errors.New("prevention provider initialization unavailable")
		}
	}
	executable, err := os.Executable()
	if err != nil {
		runner.refusal = errors.New("analysis MCP executable unavailable")
	} else {
		runner.executable = executable
	}
	instructions, err := instructionpackage.New(ctx, paths)
	if err != nil {
		return nil, err
	}
	return &Service{DB: db, Source: source, Runner: runner, Redactor: redactor, Instructions: instructions, Provider: kind, Model: model}, nil
}

type configuredSource struct {
	db       *store.DB
	resolver secrets.Resolver
	redactor *redact.Redactor
	logger   *slog.Logger
}

func (c *configuredSource) Collect(ctx context.Context, scope Scope) (Manifest, error) {
	repo, err := c.db.ResolvePreventionRepository(ctx, scope.Repository)
	if err != nil {
		return Manifest{}, fmt.Errorf("analysis requires one unambiguous registered GitHub repository: %w", err)
	}
	version, err := c.db.CurrentConnection(ctx, repo.ID, store.TicketConnection)
	if err != nil {
		return Manifest{}, fmt.Errorf("analysis connection lookup failed: %w", err)
	}
	if !version.Enabled || version.Provider != ticket.GitHub || !strings.EqualFold(version.Destination, scope.Repository) {
		return Manifest{}, errors.New("selected repository requires an enabled matching GitHub ticket connection")
	}
	factory := projectconnection.NewFactory(projectconnection.Options{DB: c.db, Resolver: c.resolver, Logger: c.logger, Redactor: c.redactor})
	budget := &github.AnalysisBudget{APIBase: string(version.Origin), Repository: scope.Repository, MaxRequests: scope.Limits.Requests, MaxResponseBytes: scope.Limits.ResponseBytes, MaxSourceBytes: scope.Limits.SourceBytes}
	ctx = github.WithAnalysisBudget(ctx, budget)
	clients, err := factory.Clients(ctx, version, 0)
	var collector Collector
	collector.Local = localStore{db: c.db, repoID: repo.ID}
	collector.Redactor = c.redactor
	collector.Budget = budget
	collector.APIBase = string(version.Origin)
	origin, parseErr := url.Parse(string(version.Origin))
	if parseErr != nil {
		return Manifest{}, errors.New("invalid configured GitHub origin")
	}
	collector.WebHost = origin.Host
	if strings.EqualFold(origin.Hostname(), "api.github.com") {
		collector.WebHost = "github.com"
	}
	if err == nil {
		collector.GitHub = clients.Code
	}
	manifest, collectErr := collector.Collect(ctx, scope)
	manifest.ConnectionVersionID = version.ID
	if err != nil {
		gap := Coverage{Source: "configured GitHub connection", Status: "unavailable", Reason: "connection credential or access verification unavailable"}
		if budget.RateLimited || errors.Is(err, github.ErrRateLimited) {
			gap.Status = "limited"
			gap.Reason = fmt.Sprintf("remote rate limit during connection verification; no retry; retry after %s; reset %s", budget.RetryAfter, budget.RetryReset.UTC().Format(time.RFC3339))
		}
		manifest.Coverage = append(manifest.Coverage, gap)
	}
	return manifest, collectErr
}

type localStore struct {
	db     *store.DB
	repoID int64
}

func (l localStore) ReadBugEvidence(ctx context.Context, repository string, issue, byteLimit int) ([]LocalObservation, error) {

	records, err := l.db.ReadPreventionRepositoryEvidence(ctx, l.repoID, repository, issue, 5, byteLimit)
	out := make([]LocalObservation, 0, len(records))
	for _, r := range records {
		out = append(out, LocalObservation{Kind: r.Kind, Locator: fmt.Sprintf("graph:%d/%s:%s", r.InstanceID, r.Kind, r.Revision), Revision: r.Revision, ReviewStatus: r.ReviewStatus, Body: string(r.Body)})
	}
	return out, err
}

type preventionProvider interface {
	PreventionIsolation(context.Context) (string, error)
	RunPrevention(context.Context, agent.PreventionRequest) (agent.DoctorRunResult, error)
}
type nativeRunner struct {
	providerRunner              preventionProvider
	paths                       config.Paths
	executable, model, provider string
	refusal                     error
}

func (r *nativeRunner) Isolation(ctx context.Context) (string, error) {
	if r.refusal != nil {
		return "", r.refusal
	}
	if r.provider != "claude" || r.providerRunner == nil || r.executable == "" {
		return "", errors.New("provider_unavailable: selected provider does not support isolated prevention analysis")
	}
	return r.providerRunner.PreventionIsolation(ctx)
}
func (r *nativeRunner) Run(ctx context.Context, i Invocation, s *Session) (_ agent.DoctorRunResult, err error) {
	if _, err = r.Isolation(ctx); err != nil {
		return agent.DoctorRunResult{}, err
	}
	if err = os.MkdirAll(r.paths.SubmissionDir(), 0700); err != nil {
		return agent.DoctorRunResult{}, err
	}
	scratch, err := os.MkdirTemp(r.paths.SubmissionDir(), "prevention-")
	if err != nil {
		return agent.DoctorRunResult{}, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(scratch)) }()
	brokerCtx, cancelBroker := context.WithCancel(ctx)
	defer cancelBroker()
	s.mu.Lock()
	s.revoke = cancelBroker
	s.mu.Unlock()
	broker, err := submission.Start(brokerCtx, submission.Options{Directory: scratch, AttemptReference: i.AttemptID, AllowResult: true, AllowTools: true, MaxWorkers: 1, RequestTimeout: 30 * time.Second, Handler: s.handle})
	if err != nil {
		return agent.DoctorRunResult{}, err
	}
	defer func() { err = errors.Join(err, broker.Close()) }()
	launch, err := agent.NewMCPLaunchSpec(r.executable, broker.DescriptorPath(), i.AttemptID, agent.PreventionToolNames()...)
	if err != nil {
		return agent.DoctorRunResult{}, err
	}
	return r.providerRunner.RunPrevention(ctx, agent.PreventionRequest{Invocation: agent.DoctorRequest{Attempt: i.AttemptID, Model: r.model, Prompt: i.Prompt, WorkingDirectory: scratch, MCPLaunch: launch, Usage: i.Usage, UsageAttemptID: i.AttemptID}, MaxTurns: i.Limits.Turns})
}
