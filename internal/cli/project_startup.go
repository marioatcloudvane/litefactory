package cli

import (
	"context"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/factory/localadmin"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

func resolveProjectStartupToken(ctx context.Context, env *Env, db *store.DB, _ *config.Config, legacyErr error) (secrets.Token, localadmin.StartupConnection, error) {
	return localadmin.ResolveProjectStartupToken(ctx, localadmin.StartupOptions{Keychain: env.Keychain, Transport: env.TeamTransport, UserAgent: initUserAgent(env)}, db, legacyErr)
}
