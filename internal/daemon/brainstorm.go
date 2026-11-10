package daemon

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/brainstorm"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teampackage"
	"github.com/cloudvane/litefactory/internal/turn"
)

// prepareBrainstorm wires the brainstorm service: installed-team capture, the
// read-only scan runner, issue search, consent checks and redaction, then
// registers it as the brainstorm_scan coordinator and publishes it to the connector.
func (d *Daemon) prepareBrainstorm(judge *turn.JudgeService, sanitizer *redact.Redactor, now func() time.Time) error {
	if d.remoteConnector == nil || d.remoteConnector.actions == nil || d.remoteConnector.actions.workflows == nil {
		return nil
	}
	invocation, err := factoryAdvisoryInvocation(judge.Config)
	if err != nil {
		return err
	}
	runner := &turn.BrainstormScanRunner{DB: d.db, Paths: judge.Paths, Executable: judge.Executable, Providers: judge.Providers, Git: judge.Git, Provider: agent.Kind(invocation.Provider), Model: invocation.Model, Thinking: invocation.Thinking, Deadline: 30 * time.Minute}
	if !runner.Ready() {
		return errors.New("isolated brainstorm scan provider unavailable")
	}
	packages, err := teampackage.New(teampackage.Options{Paths: judge.Paths, Validate: turn.ValidateTeamWorkflows, InUse: func(ctx context.Context, id teampackage.Identity) ([]string, error) {
		return d.db.RepositoryTeamUses(ctx, id.Source, id.Slug, id.Version)
	}})
	if err != nil {
		return err
	}
	clock := now
	if clock == nil {
		clock = time.Now
	}
	db := d.db
	service, err := brainstorm.New(brainstorm.Options{
		DB:       db,
		Now:      clock,
		Team:     brainstorm.InstalledTeam{DB: db, Packages: packages},
		Runner:   runner,
		Issues:   brainstorm.TrackerSearcher{Select: d.brainstormIssueSelector()},
		Deadline: runner.Deadline,
		Protect: func(ctx context.Context, text string) (string, error) {
			b, e := sanitizer.Apply(ctx, text, max(4096, len(text)+redact.Margin))
			if e == nil && b.Truncated() {
				e = errors.New("brainstorm redaction exceeded bound")
			}
			return b.String(), e
		},
		Consent: func(ctx context.Context, p brainstorm.Principal, repositoryRef string) error {
			return brainstormConsent(ctx, db, p, repositoryRef, clock())
		},
	})
	if err != nil {
		return err
	}
	if err = d.remoteConnector.actions.workflows.Register(service); err != nil {
		return err
	}
	d.remoteConnector.server.SetBrainstorm(service)
	d.remoteConnector.actions.brainstormDrafts.bind(service)
	return nil
}

// reportBrainstormUnavailable tells the connector why brainstorm is off, so
// clients are told the reason instead of inferring a permissions problem.
func (d *Daemon) reportBrainstormUnavailable(ctx context.Context, cause error, sanitizer *redact.Redactor) {
	if d.remoteConnector == nil || d.remoteConnector.server == nil || cause == nil {
		return
	}
	reason := "brainstorm setup failed"
	if b, err := sanitizer.Apply(ctx, cause.Error(), 4096); err == nil && !b.Truncated() && b.String() != "" {
		reason = b.String()
	}
	d.remoteConnector.server.SetBrainstormUnavailable(reason)
}

// factoryAdvisoryInvocation resolves the agent for factory-level advisory runs
// (brainstorm scans, task discussion) that have no task of their own. An
// explicit judge.agent wins; without one they use the factory's default agent,
// the one ordinary runs use.
func factoryAdvisoryInvocation(cfg *config.Config) (config.JudgeInvocation, error) {
	defaultAgent, _ := cfg.DefaultAgent()
	return cfg.ResolveJudge(config.JudgeOptions{}, defaultAgent, "")
}

// brainstormConsent checks the principal's live grant holds repository:brainstorm for the repository.
func brainstormConsent(ctx context.Context, db *store.DB, p brainstorm.Principal, repositoryRef string, at time.Time) error {
	return db.CheckBrainstormConsent(ctx, store.OwnerActionPrincipal{OwnerID: p.OwnerID, OwnerGeneration: p.OwnerGeneration, Transport: p.Transport, ClientID: p.ClientID, GrantID: p.GrantID}, repositoryRef, at)
}
