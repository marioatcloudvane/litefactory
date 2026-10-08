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
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
)

// configureDoctorNative resolves installed provider transports only. Their
// provider-owned isolation attestations still gate every diagnosis/repair launch.
func configureDoctorNative(ctx context.Context, service *doctor.Service, env *Env) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	providers := make(map[string]agent.DoctorRunner)
	repairs := make(map[string]agent.DoctorRepairRunner)
	cfg := service.Config
	environ := os.Environ()
	// Missing forge credentials leave an explicit tool coverage error; they must
	// not discard owner input or prevent diagnosis from inspecting local sources.
	ref, configured := secrets.ReferenceFrom(cfg.Providers.GitHub.TokenKeychain.Service, cfg.Providers.GitHub.TokenKeychain.Account, cfg.Providers.GitHub.TokenEnv)
	if configured {
		if token, resolveErr := (secrets.Resolver{Keychain: env.Keychain}).Resolve(ctx, ref); resolveErr == nil {
			service.Redactor = redact.New(redact.Options{Logger: env.Logger, Token: token, Environ: environ})
			if client, clientErr := github.New(github.Options{APIBase: cfg.Providers.GitHub.APIBase, Token: token, Logger: env.Logger, UserAgent: initUserAgent(env)}); clientErr == nil {
				service.Comments = client
			}
		}
	}
	if cfg.Agents.Claude != nil {
		model, _ := cfg.Tiers.ResolveModel(config.Kind("claude"), config.TierFlagship)
		runner, e := harness.New(harness.Options{Paths: service.Paths, Claude: *cfg.Agents.Claude, Model: model, Environ: environ, Logger: env.Logger})
		if e == nil {
			providers["claude"] = runner
			repairs["claude"] = runner
		}
	}
	if cfg.Agents.Codex != nil {
		model, _ := cfg.Tiers.ResolveModel(config.Kind("codex"), config.TierFlagship)
		runner, e := codex.New(codex.Options{Paths: service.Paths, Codex: *cfg.Agents.Codex, Model: model, Environ: environ, Logger: env.Logger})
		if e == nil {
			providers["codex"] = runner
			repairs["codex"] = runner
		}
	}
	service.Runner = &doctor.Adapters{Providers: providers, Paths: service.Paths, Executable: executable}
	service.RepairRunner = &doctor.RepairAdapters{Providers: repairs, Paths: service.Paths, Executable: executable}
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
		_, err = service.DispatchRepairRestart(ctx, request.Target)
	}
	saved, statusErr := service.ReadConversation(ctx, request.Target)
	if statusErr == nil {
		statusErr = renderDoctor(env.Stdout, saved)
	}
	return errors.Join(err, statusErr)
}
