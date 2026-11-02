package cli

import (
	"context"
	"errors"
	"os"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/cimonitor"
	"github.com/cloudvane/litefactory/internal/codex"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/factory/localdoctor"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
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
		runner, e := harness.New(harness.Options{Paths: service.Paths, Claude: *cfg.Agents.Claude, Model: model, Environ: environ, Logger: env.Logger, ThinkingLogs: cfg.Settings.ThinkingLogs})
		if e == nil {
			providers["claude"] = runner
			repairs["claude"] = runner
			advisors["claude"] = runner
		}
	}
	if cfg.Agents.Codex != nil {
		model, _ := cfg.Tiers.ResolveModel(config.Kind("codex"), config.TierFlagship)
		runner, e := codex.New(codex.Options{Paths: service.Paths, Codex: *cfg.Agents.Codex, Model: model, Environ: environ, Logger: env.Logger, ThinkingLogs: cfg.Settings.ThinkingLogs})
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
		if botID, e := localdoctor.BotID(ctx, service.DB); e == nil {
			effects.BotID = botID
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
	ref, configured := secrets.ReferenceFrom(cfg.Providers.GitHub.TokenKeychain.Service, cfg.Providers.GitHub.TokenKeychain.Account, cfg.Providers.GitHub.TokenEnv)
	var token secrets.Token
	if configured {
		token, _ = (secrets.Resolver{Keychain: env.Keychain}).Resolve(ctx, ref)
		service.Redactor.AddToken(token)
	}
	service.CIRepository = cfg.Alerts.CI.Repository
	apiOrigin, ciOriginErr := ticket.CanonicalOrigin(cfg.Providers.GitHub.APIBase)
	if ciOriginErr != nil {
		return ciOriginErr
	}
	origin := string(apiOrigin)
	ciClient, ciErr := github.New(github.Options{APIBase: origin, Token: token, Redactor: service.Redactor, Logger: env.Logger})
	observer := &cimonitor.Observer{Target: cfg.Alerts.CI, Origin: origin, Gate: &cimonitor.Gate{Store: service.DB, Key: "ci:" + origin}}
	if ciErr == nil {
		observer.Reader = ciClient
	}
	service.CIProbe = observer
	botID, err := localdoctor.BotID(ctx, service.DB)
	if errors.Is(err, store.ErrNotFound) {
		return nil // Local diagnosis remains available without forge coverage.
	}
	if err != nil {
		return err
	}
	factory := projectconnection.NewFactory(projectconnection.Options{DB: service.DB, Resolver: secrets.Resolver{Keychain: env.Keychain}, Logger: env.Logger, Redactor: service.Redactor, ExpectedActorID: botID, LegacyReference: ref, LegacyToken: token})
	if err := projectconnection.Initialize(ctx, service.DB, *cfg, factory); err != nil {
		return err
	}
	clients, err := localdoctor.BootstrapClients(ctx, service.DB, factory)
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
		return doctorApplyRefusal(ctx, service, request, err)
	}
	defer func() { err = errors.Join(err, closeHost()) }()
	applied, err := localdoctor.Apply(ctx, service, request)
	if !applied {
		return err
	}
	saved, statusErr := localdoctor.ReadConversation(ctx, service, request.Target)
	if statusErr == nil {
		saved = localdoctor.RequireRepairInspection(saved, err != nil)
		statusErr = renderDoctor(env.Stdout, saved)
	}
	return errors.Join(err, statusErr)
}

// Capability refusals occur before approval or repair reservation. A retained
// repair belongs to an earlier command and must not be attributed to this one.
func doctorApplyRefusal(ctx context.Context, service *doctor.Service, request doctor.ApplyRequest, cause error) error {
	return localdoctor.ApplyRefusal(ctx, service, request, cause)
}
