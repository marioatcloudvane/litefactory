package cli

import (
	"context"
	"github.com/cloudvane/litefactory/internal/factory/localanalysis"
	"github.com/cloudvane/litefactory/internal/loganalysis"
)

func configureLogFiling(ctx context.Context, env *Env, rt *repoRuntime, svc *loganalysis.Service, ids []int64) {
	localanalysis.ConfigureFiling(ctx, localanalysis.FilingOptions{DB: rt.db, Config: rt.cfg, Logger: env.Logger, Keychain: env.Keychain}, svc, ids)
}
