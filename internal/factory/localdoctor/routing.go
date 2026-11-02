// Package localdoctor owns local diagnostic workflow operations.
package localdoctor

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/store"
)

// BootstrapClients selects retained execution routing before current configuration.
func BootstrapClients(ctx context.Context, db *store.DB, factory *projectconnection.Factory) (*projectconnection.Clients, error) {
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

// BotID supplies the current retained factory publishing identity.
func BotID(ctx context.Context, db *store.DB) (int64, error) {
	bot, err := db.GetIdentity(ctx, store.ForgeGitHub, store.RoleBot)
	return bot.UserID, err
}
