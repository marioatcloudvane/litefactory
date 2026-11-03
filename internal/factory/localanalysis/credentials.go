package localanalysis

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

// InitializeCredentials collects the selected repositories' retained secrets before exposing log evidence.
func InitializeCredentials(ctx context.Context, keychain func(context.Context, string, string) (string, error), db *store.DB, cfg *config.Config, ids []int64, r *redact.Redactor) error {
	resolver := secrets.Resolver{Keychain: keychain}
	register := func(ref secrets.Reference) error {
		token, err := resolver.Resolve(ctx, ref)
		if err != nil {
			return errors.New("mcp logs cannot initialize configured credential redaction")
		}
		r.AddToken(token)
		return nil
	}
	if ref, ok := secrets.ReferenceFrom(cfg.Providers.GitHub.TokenKeychain.Service, cfg.Providers.GitHub.TokenKeychain.Account, cfg.Providers.GitHub.TokenEnv); ok {
		if err := register(ref); err != nil {
			return err
		}
	}
	for _, id := range ids {
		versions, err := db.ListConnections(ctx, id)
		if err != nil {
			return errors.New("mcp logs credential registry unavailable")
		}
		for _, v := range versions {
			if ref, ok := secrets.ReferenceFrom(v.CredentialService, v.CredentialAccount, v.CredentialEnv); ok {
				if err := register(ref); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
