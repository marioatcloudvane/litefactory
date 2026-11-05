package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/cloudvane/litefactory/internal/brainstorm"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/loganalysis"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// projectSecrets registers one project's connection credentials with the
// shared redactor; loading a connection's clients does that.
func projectSecrets(db *store.DB, connections *projectconnection.Factory) func(context.Context, int64) error {
	return func(ctx context.Context, id int64) error {
		versions, e := db.ListConnections(ctx, id)
		if e != nil {
			return errors.New("project credential registry unavailable")
		}
		for _, version := range versions {
			if _, e = connections.Clients(ctx, version, 0); e != nil {
				return errors.New("project credential redaction could not be initialized")
			}
		}
		return nil
	}
}

// brainstormIssueSelector picks the issue tracker of a brainstorm repository's
// project by the rule log analysis uses ([loganalysis.TicketTracker]); a
// project with no GitHub or Linear ticket connection gets [brainstorm.ErrNoTracker].
func (d *Daemon) brainstormIssueSelector() func(context.Context, string) (loganalysis.Tracker, error) {
	return projectTicketTracker(d.db, d.connections)
}

func projectTicketTracker(db *store.DB, connections *projectconnection.Factory) func(context.Context, string) (loganalysis.Tracker, error) {
	return func(ctx context.Context, repositoryRef string) (loganalysis.Tracker, error) {
		forge, slug, ok := strings.Cut(repositoryRef, ":")
		if !ok {
			forge, slug = store.ForgeGitHub, repositoryRef
		}
		owner, name, ok := strings.Cut(slug, "/")
		if !ok || owner == "" || name == "" {
			return nil, brainstorm.ErrInvalid
		}
		repo, err := db.GetRepoBySlug(ctx, forge, owner, name)
		if errors.Is(err, store.ErrNotFound) {
			return nil, brainstorm.ErrNotFound
		}
		if err != nil {
			return nil, errors.New("project registry unavailable")
		}
		versions, err := db.ListConnections(ctx, repo.ID)
		if err != nil {
			return nil, errors.New("project credential registry unavailable")
		}
		var selected loganalysis.Tracker
		for _, version := range versions {
			if version.Role != store.TicketConnection {
				continue
			}
			if connections == nil {
				return nil, errors.New("project connections unavailable")
			}
			clients, e := connections.Clients(ctx, version, 0)
			if e != nil {
				return nil, errors.New("project ticket connection could not be loaded")
			}
			if tracker, ok := loganalysis.TicketTracker(clients.Linear, clients.Code, version.Destination); ok {
				selected = tracker
			}
		}
		if selected == nil {
			return nil, brainstorm.ErrNoTracker
		}
		return selected, nil
	}
}

// newConversationAnalyses wires the log analysis service that
// `standards analyze logs` uses for analyses the owner starts in the conversation.
func newConversationAnalyses(s Setup, connections *projectconnection.Factory, sharedRedactor *redact.Redactor, gitBinary *git.Binary) (*ConversationAnalyses, error) {
	if s.DB == nil || s.Config == nil || connections == nil || sharedRedactor == nil {
		return nil, errors.New("initialized log analysis runtime required")
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	model, _ := s.Config.Tiers.ResolveModel(config.Kind("claude"), config.TierFlagship)
	logs := &loganalysis.Service{DB: s.DB, Redactor: sharedRedactor, RunsRoot: s.Paths.RunsDir(), Executable: executable, Model: model, DisableMCP: !s.Config.Settings.MCPAutoStart, Git: gitBinary}
	if kind, ok := s.Config.DefaultAgent(); ok && kind == config.Kind("claude") && s.Config.Agents.Claude != nil {
		caches, e := s.Paths.ResolveGoBuildCaches(s.Config.Settings)
		if e != nil {
			return nil, e
		}
		runner, e := harness.New(harness.Options{GoBuildCaches: caches, Paths: s.Paths, Claude: *s.Config.Agents.Claude, Model: model, Environ: s.Environ, Logger: s.Logger, ThinkingLogs: s.Config.Settings.ThinkingLogs})
		if e != nil {
			return nil, e
		}
		logs.Runner = runner
	}
	if owner, name, ok := s.Config.Standards.LogAnalysis.UpstreamRepository(); ok {
		logs.UpstreamName = "github:" + owner + "/" + name
		if !s.Token.Empty() {
			client, e := github.New(github.Options{Token: s.Token, APIBase: s.Config.Providers.GitHub.APIBase, Redactor: sharedRedactor, Logger: s.Logger, UserAgent: s.UserAgent})
			if e != nil {
				return nil, e
			}
			logs.Upstream = loganalysis.GitHubTracker{Client: client, Repo: github.Repo{Owner: owner, Name: name}}
		}
	}
	configure := func(ctx context.Context, ids []int64) (*loganalysis.Service, error) {
		// Log bodies can contain any project credential; loading each connection
		// registers its secrets with the shared redactor. Each project's ticket
		// source is where its Bugs are searched and filed.
		svc := *logs
		svc.Projects = map[int64]loganalysis.Tracker{}
		for _, id := range ids {
			versions, e := s.DB.ListConnections(ctx, id)
			if e != nil {
				return nil, errors.New("project credential registry unavailable")
			}
			for _, version := range versions {
				clients, e := connections.Clients(ctx, version, 0)
				if e != nil {
					return nil, errors.New("project credential redaction could not be initialized")
				}
				if version.Role != store.TicketConnection {
					continue
				}
				if tracker, ok := loganalysis.TicketTracker(clients.Linear, clients.Code, version.Destination); ok {
					svc.Projects[id] = tracker
				}
			}
		}
		return &svc, nil
	}
	start := func(ctx context.Context, q loganalysis.Request, dryRun bool) (store.LogAnalysis, error) {
		q.DryRun = dryRun
		svc, e := configure(ctx, q.RepositoryIDs)
		if e != nil {
			return store.LogAnalysis{}, e
		}
		return svc.Start(ctx, q)
	}
	recoverFilings := func(ctx context.Context, id string) error {
		analysis, e := s.DB.GetLogAnalysis(ctx, id)
		if e != nil {
			return e
		}
		var scope loganalysis.Scope
		if e = json.Unmarshal(analysis.Scope, &scope); e != nil {
			return e
		}
		svc, e := configure(ctx, scope.Request.RepositoryIDs)
		if e != nil {
			return e
		}
		return svc.ReconcileWorkflowFilings(ctx, id)
	}
	targets := func(ctx context.Context, id int64) (string, error) {
		versions, e := s.DB.ListConnections(ctx, id)
		if e != nil {
			return "", e
		}
		destinations := []map[string]any{}
		for _, v := range versions {
			if v.Role == store.TicketConnection {
				destinations = append(destinations, map[string]any{"connection_id": v.ID, "revision": v.Revision, "provider": v.Provider, "origin": v.Origin, "destination": v.Destination, "enabled": v.Enabled})
			}
		}
		raw, e := json.Marshal(map[string]any{"project": destinations, "upstream": logs.UpstreamName})
		return string(raw), e
	}
	report := func(ctx context.Context, id string) (string, error) {
		view, e := logs.Inspect(ctx, id, 0, 1)
		if e != nil {
			return "", e
		}
		return renderConversationAnalysis(view), nil
	}
	return &ConversationAnalyses{DB: s.DB, Start: start, Report: report, Log: s.Logger, RecoverFilings: recoverFilings, Targets: targets}, nil
}
