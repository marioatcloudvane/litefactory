package cli

import (
	"context"
	"errors"
	"os"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/codex"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// configureDoctorNative resolves installed provider transports only. Their
// provider-owned isolation attestations still gate every diagnosis/repair launch.
func configureDoctorNative(ctx context.Context, service *doctor.Service, env *Env) error {
	service.BuildVersion = env.Version
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	providers := make(map[string]agent.DoctorRunner)
	repairs := make(map[string]agent.DoctorRepairRunner)
	advisors := make(map[string]turn.ConflictAdvisor)
	cfg := service.Config
	environ := os.Environ()
	if err := configureDoctorTicketWorkflow(ctx, service, env, environ); err != nil {
		return err
	}
	if cfg.Agents.Claude != nil {
		model, _ := cfg.Tiers.ResolveModel(config.Kind("claude"), config.TierFlagship)
		runner, e := harness.New(harness.Options{Paths: service.Paths, Claude: *cfg.Agents.Claude, Model: model, Environ: environ, Logger: env.Logger})
		if e == nil {
			providers["claude"] = runner
			repairs["claude"] = runner
			advisors["claude"] = runner
		}
	}
	if cfg.Agents.Codex != nil {
		model, _ := cfg.Tiers.ResolveModel(config.Kind("codex"), config.TierFlagship)
		runner, e := codex.New(codex.Options{Paths: service.Paths, Codex: *cfg.Agents.Codex, Model: model, Environ: environ, Logger: env.Logger})
		if e == nil {
			providers["codex"] = runner
			repairs["codex"] = runner
			advisors["codex"] = runner
		}
	}
	service.Runner = &doctor.Adapters{Providers: providers, Paths: service.Paths, Executable: executable}
	service.RepairRunner = &doctor.RepairAdapters{Providers: repairs, Paths: service.Paths, Executable: executable}
	binary, gitErr := git.Resolve(ctx)
	if gitErr == nil {
		effects := &daemon.FactoryDoctorRepairEffects{DB: service.DB, Git: binary, CloneGate: daemon.NewDoctorRepairCloneGate(), Advisors: advisors, Redactor: service.Redactor}
		effects.Comments, _ = service.Comments.(daemon.DoctorRepairComments)
		if bot, e := service.DB.GetIdentity(ctx, store.ForgeGitHub, store.RoleBot); e == nil {
			effects.BotID = bot.UserID
		}
		service.RepairEffects = effects
	}
	return nil
}

// A missing legacy credential must not hide a usable retained project client.
// The routed bridge selects the original execution version for every effect.
func configureDoctorTicketWorkflow(ctx context.Context, service *doctor.Service, env *Env, environ []string) error {
	ctx, cancel := context.WithTimeout(ctx, projectconnection.DefaultTimeout)
	defer cancel()
	cfg := service.Config
	service.Redactor = redact.New(redact.Options{Logger: env.Logger, Environ: environ})
	bot, err := service.DB.GetIdentity(ctx, store.ForgeGitHub, store.RoleBot)
	if errors.Is(err, store.ErrNotFound) {
		return nil // Local diagnosis remains available without forge coverage.
	}
	if err != nil {
		return err
	}
	ref, configured := secrets.ReferenceFrom(cfg.Providers.GitHub.TokenKeychain.Service, cfg.Providers.GitHub.TokenKeychain.Account, cfg.Providers.GitHub.TokenEnv)
	var token secrets.Token
	if configured {
		token, _ = (secrets.Resolver{Keychain: env.Keychain}).Resolve(ctx, ref)
		service.Redactor.AddToken(token)
	}
	factory := projectconnection.NewFactory(projectconnection.Options{DB: service.DB, Resolver: secrets.Resolver{Keychain: env.Keychain}, Logger: env.Logger, Redactor: service.Redactor, ExpectedActorID: bot.UserID, LegacyReference: ref, LegacyToken: token})
	if err := projectconnection.Initialize(ctx, service.DB, *cfg, factory); err != nil {
		return err
	}
	clients, err := doctorBootstrapClients(ctx, service.DB, factory)
	if errors.Is(err, store.ErrNotFound) {
		return nil // The read tools report unavailable remote coverage.
	}
	if err != nil {
		return err
	}
	selector := daemon.ProjectTicketSelector(service.DB, factory)
	workflow, err := turn.NewRoutedGitHubTicketWorkflow(service.DB, clients.Tickets, clients.Actor.ID, selector)
	if err != nil {
		return err
	}
	service.Comments = workflow
	return nil
}

// Bootstrap metadata is only used to construct the bridge. Its selector still
// requires the retained session context before any remote operation.
func doctorBootstrapClients(ctx context.Context, db *store.DB, factory *projectconnection.Factory) (*projectconnection.Clients, error) {
	projects, err := db.ListRepos(ctx)
	if err != nil {
		return nil, err
	}
	for _, project := range projects {
		sessions, err := db.ListRepoSessions(ctx, project.ID)
		if err != nil {
			return nil, err
		}
		for _, session := range sessions {
			snapshot, err := db.GetExecutionSnapshot(ctx, session.ID)
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			version, err := db.GetConnectionVersion(ctx, snapshot.TicketVersionID)
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if clients, err := factory.Clients(ctx, version, snapshot.ActorID); err == nil {
				return clients, nil
			}
		}
		version, err := db.CurrentConnection(ctx, project.ID, store.TicketConnection)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !version.Enabled {
			continue
		}
		if clients, err := factory.Clients(ctx, version, 0); err == nil {
			return clients, nil
		}
	}
	return nil, store.ErrNotFound
}

func configureDoctorHost(service *doctor.Service) (func() error, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	replacement, err := daemon.NewDoctorServiceReplacement(service.DB, executable, service.Paths, os.Environ())
	if err != nil {
		return nil, err
	}
	environment, route := daemon.NewDoctorHostCapabilities(service.DB, service.Paths, replacement)
	service.RepairEnvironment = environment
	service.RestartRoute = route
	return environment.Close, nil
}

func applyDoctorCLI(ctx context.Context, service *doctor.Service, env *Env, request doctor.ApplyRequest) (err error) {
	closeHost, err := configureDoctorHost(service)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeHost()) }()
	if err = service.CheckRepairCapability(ctx, request.Target); err != nil {
		return err
	}
	request.PresentationID, err = service.DB.CurrentDoctorLocalPresentation(ctx, request.Target.String())
	if err != nil {
		return errors.New("doctor apply requires a fully displayed current proposal; run doctor status first")
	}
	_, err = service.Apply(ctx, request)
	if err == nil {
		operation, readErr := service.DB.ReadDoctorRepair(ctx, request.Target.String())
		if readErr == nil && operation.State == "restart_pending" {
			_, err = service.DispatchRepairRestart(ctx, request.Target)
		}
	}
	saved, statusErr := service.ReadConversation(ctx, request.Target)
	if statusErr == nil {
		statusErr = renderDoctor(env.Stdout, saved)
	}
	return errors.Join(err, statusErr)
}
