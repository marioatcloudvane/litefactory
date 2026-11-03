package localanalysis

import (
	"context"
	"github.com/cloudvane/litefactory/internal/config"
	"log/slog"
	"strings"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/loganalysis"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

// FilingOptions supplies trusted local filing dependencies.
type FilingOptions struct {
	DB       *store.DB
	Config   *config.Config
	Logger   *slog.Logger
	Keychain func(context.Context, string, string) (string, error)
}

// ConfigureFiling gives an analysis read access to its repositories and the
// trackers it may search and file into: each repository's current ticket
// connection and, when configured, the upstream repository with the factory's
// GitHub credential. Anything unavailable stays nil and is reported as such.
func ConfigureFiling(ctx context.Context, options FilingOptions, svc *loganalysis.Service, ids []int64) {
	if binary, e := git.Resolve(ctx); e == nil {
		svc.Git = binary
	}
	resolver := secrets.Resolver{Keychain: options.Keychain}
	forge := options.Config.Providers.GitHub
	ref, configured := secrets.ReferenceFrom(forge.TokenKeychain.Service, forge.TokenKeychain.Account, forge.TokenEnv)
	var token secrets.Token
	if configured {
		token, _ = resolver.Resolve(ctx, ref)
		svc.Redactor.AddToken(token)
	}
	factory := projectconnection.NewFactory(projectconnection.Options{DB: options.DB, Resolver: resolver, Logger: options.Logger, Redactor: svc.Redactor, LegacyReference: ref, LegacyToken: token})
	svc.Projects = map[int64]loganalysis.Tracker{}
	for _, id := range ids {
		version, e := options.DB.CurrentConnection(ctx, id, store.TicketConnection)
		if e != nil {
			continue
		}
		clients, e := factory.Clients(ctx, version, 0)
		if e != nil {
			options.Logger.WarnContext(ctx, "log analysis: ticket source unavailable", "repository_id", id, "err", e)
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
	owner, name, ok := options.Config.Standards.LogAnalysis.UpstreamRepository()
	if !ok {
		return
	}
	svc.UpstreamName = "github:" + owner + "/" + name
	if token.Empty() {
		return
	}
	client, e := github.New(github.Options{Token: token, APIBase: forge.APIBase, Redactor: svc.Redactor, Logger: options.Logger})
	if e == nil {
		svc.Upstream = loganalysis.GitHubTracker{Client: client, Repo: github.Repo{Owner: owner, Name: name}}
	}
}
