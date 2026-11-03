// Package localprojects owns local project connection, binding and approver administration.
package localprojects

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

// Options contains trusted local host dependencies.
type Options struct {
	UserAgent     string
	KeychainWrite func(context.Context, string, string, string) error
	Logger        *slog.Logger
	Keychain      func(context.Context, string, string) (string, error)
}

// Service owns bounded local administration operations.
type Service struct {
	db      *store.DB
	repo    store.Repo
	options Options
}

// Open resolves the registered project for local administration.
func Open(ctx context.Context, db *store.DB, projectID int64, options Options) (*Service, error) {
	repo, err := db.GetRepo(ctx, projectID)
	if err != nil {
		return nil, unavailable(err)
	}
	return &Service{db: db, repo: repo, options: options}, nil
}
func unavailable(err error) error {
	if err == nil {
		return nil
	}
	return &contracts.ProjectUnavailable{Err: err}
}
func usage(format string, args ...any) error {
	return &contracts.ProjectUsageError{Err: fmt.Errorf(format, args...)}
}
func connectionView(v store.ConnectionVersion) contracts.ProjectConnectionView {
	return contracts.ProjectConnectionView{ID: v.ID, RepoID: v.RepoID, Revision: v.Revision, Role: string(v.Role), Provider: v.Provider, Origin: v.Origin, Destination: v.Destination, Enabled: v.Enabled, Provenance: v.Provenance}
}
func (s *Service) factory(expectedActorID int64) *projectconnection.Factory {
	return projectconnection.NewFactory(projectconnection.Options{DB: s.db, Logger: s.options.Logger, ExpectedActorID: expectedActorID, Resolver: secrets.Resolver{Keychain: s.options.Keychain}})
}

func unavailableText(message string) error { return unavailable(fmt.Errorf("%s", message)) }
