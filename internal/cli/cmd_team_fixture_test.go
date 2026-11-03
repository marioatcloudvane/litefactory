package cli

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
	"os"
)

// openTeamBindingDB opens fixture storage; production binding access belongs
// to factory/localteams and is not exposed through a consumer database port.
func openTeamBindingDB(ctx context.Context, paths config.Paths) (*store.DB, error) {
	info, err := os.Lstat(paths.DBPath())
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("repository database is not an ordinary file")
	}
	db, err := store.Open(ctx, paths.DBPath())
	if err != nil {
		return nil, err
	}
	if err = db.Migrate(ctx); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return db, nil
}
