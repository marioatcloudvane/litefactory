package daemon

import (
	"context"
	"errors"
	"os"
	"strings"

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
		runner, e := harness.New(harness.Options{Paths: s.Paths, Claude: *s.Config.Agents.Claude, Model: model, Environ: s.Environ, Logger: s.Logger, ThinkingLogs: s.Config.Settings.ThinkingLogs})
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
	start := func(ctx context.Context, q loganalysis.Request, dryRun bool) (store.LogAnalysis, error) {
		// Log bodies can contain any project credential; loading each connection
		// registers its secrets with the shared redactor. Each project's ticket
		// source is where its Bugs are searched and filed.
		q.DryRun = dryRun
		svc := *logs
		svc.Projects = map[int64]loganalysis.Tracker{}
		for _, id := range q.RepositoryIDs {
			versions, e := s.DB.ListConnections(ctx, id)
			if e != nil {
				return store.LogAnalysis{}, errors.New("project credential registry unavailable")
			}
			for _, version := range versions {
				clients, e := connections.Clients(ctx, version, 0)
				if e != nil {
					return store.LogAnalysis{}, errors.New("project credential redaction could not be initialized")
				}
				if version.Role != store.TicketConnection {
					continue
				}
				owner, name, _ := strings.Cut(version.Destination, "/")
				switch {
				case clients.Linear != nil:
					svc.Projects[id] = loganalysis.LinearTracker{Client: clients.Linear}
				case clients.Code != nil:
					svc.Projects[id] = loganalysis.GitHubTracker{Client: clients.Code, Repo: github.Repo{Owner: owner, Name: name}}
				}
			}
		}
		return svc.Start(ctx, q)
	}
	report := func(ctx context.Context, id string) (string, error) {
		view, e := logs.Inspect(ctx, id, 0, 1)
		if e != nil {
			return "", e
		}
		return renderConversationAnalysis(view), nil
	}
	return &ConversationAnalyses{DB: s.DB, Start: start, Report: report, Log: s.Logger}, nil
}
