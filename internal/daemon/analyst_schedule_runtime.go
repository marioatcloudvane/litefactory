package daemon

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/cloudvane/litefactory/internal/analystschedule"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/drift"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/loganalysis"
	"github.com/cloudvane/litefactory/internal/prevention"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// newAnalystScheduleRuntime reuses the standalone analysts and connection factory;
// no schedule receives credentials, extra tools or authority to publish standards.
func newAnalystScheduleRuntime(ctx context.Context, s Setup, connections *projectconnection.Factory, sharedRedactor *redact.Redactor, gitBinary *git.Binary) (*analystschedule.Service, error) {
	if s.DB == nil || s.Config == nil || connections == nil || sharedRedactor == nil || gitBinary == nil {
		return nil, errors.New("initialized analyst schedule runtime required")
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	bugs, err := prevention.NewRuntime(ctx, s.DB, s.Config, s.Paths, s.Logger, s.ConnectionSecrets)
	if err != nil {
		return nil, err
	}
	// The source collector retains its own redactor for acquisition; the analysis
	// boundary additionally shares every credential already registered by the daemon.
	bugs.Redactor = sharedRedactor
	model, _ := s.Config.Tiers.ResolveModel(config.Kind("claude"), config.TierFlagship)
	logs := &loganalysis.Service{DB: s.DB, Redactor: sharedRedactor, RunsRoot: s.Paths.RunsDir(), Executable: executable, Model: model, DisableMCP: !s.Config.Settings.MCPAutoStart}
	var provider *harness.Runner
	kind, configured := s.Config.DefaultAgent()
	if configured && kind == config.Kind("claude") && s.Config.Agents.Claude != nil {
		provider, err = harness.New(harness.Options{Paths: s.Paths, Claude: *s.Config.Agents.Claude, Model: model, Environ: s.Environ, Logger: s.Logger})
		if err != nil {
			return nil, err
		}
		logs.Runner = provider
	}
	service := &analystschedule.Service{DB: s.DB, Logs: logs, Bugs: bugs}
	service.Authorize = func(callCtx context.Context, repo store.Repo, analyst string) error {
		current, e := s.DB.GetRepo(callCtx, repo.ID)
		if e != nil {
			return errors.New("schedule repository is no longer registered")
		}
		if current.Path != repo.Path || current.Forge != repo.Forge || current.Slug() != repo.Slug() {
			return errors.New("schedule repository registration changed")
		}
		if !s.Config.Settings.MCPAutoStart {
			return errors.New("MCP startup is disabled; scheduled analyst launch blocked")
		}
		if !configured || kind != config.Kind("claude") || provider == nil {
			return errors.New("configured provider does not support isolated scheduled analysis")
		}
		version, e := s.DB.CurrentConnection(callCtx, repo.ID, store.CodeConnection)
		if e != nil {
			return errors.New("schedule code connection unavailable")
		}
		if !version.Enabled || version.Provider != ticket.GitHub || !strings.EqualFold(version.Destination, repo.Slug()) {
			return errors.New("schedule requires an enabled matching code connection")
		}
		if observation, checkErr := connections.CheckAndSave(callCtx, s.DB, version); checkErr != nil || observation.Status != "usable_for_checked_capabilities" {
			return errors.New("schedule code connection credential or access verification unavailable")
		}
		if analyst == "bugs" {
			version, e = s.DB.CurrentConnection(callCtx, repo.ID, store.TicketConnection)
			if e != nil {
				return errors.New("schedule ticket connection unavailable")
			}
			if !version.Enabled || version.Provider != ticket.GitHub || !strings.EqualFold(version.Destination, repo.Slug()) {
				return errors.New("bug schedule requires an enabled matching GitHub ticket connection")
			}
			if observation, checkErr := connections.CheckAndSave(callCtx, s.DB, version); checkErr != nil || observation.Status != "usable_for_checked_capabilities" {
				return errors.New("schedule ticket credential or access verification unavailable")
			}
		}
		// Log bodies can contain credentials from either current project connection.
		// Resolve those through the existing background policy and redaction registry.
		versions, e := s.DB.ListConnections(callCtx, repo.ID)
		if e != nil {
			return errors.New("schedule credential registry unavailable")
		}
		for _, version := range versions {
			if _, e = connections.Clients(callCtx, version, 0); e != nil {
				return errors.New("schedule credential redaction initialization unavailable")
			}
		}
		return nil
	}
	service.Drift = func(callCtx context.Context, repo store.Repo) (*drift.Service, error) {
		version, e := s.DB.CurrentConnection(callCtx, repo.ID, store.CodeConnection)
		if e != nil {
			return nil, e
		}
		clients, e := connections.Clients(callCtx, version, 0)
		if e != nil {
			return nil, e
		}
		analysis := &drift.Service{Store: s.DB, Acquisition: &drift.Acquirer{Store: s.DB, Git: gitBinary, ClonePath: repo.Path, Discussion: clients.Code, Repo: github.Repo{Owner: repo.Owner, Name: repo.Name}, Redactor: sharedRedactor}}
		if provider != nil {
			analysis.Runner = &drift.Provider{Runner: provider, Model: model, Directory: os.TempDir(), Executable: executable}
		}
		if !s.Config.Settings.MCPAutoStart {
			analysis.LaunchRefusal = "MCP startup is disabled"
		}
		return analysis, nil
	}
	return service, nil
}
