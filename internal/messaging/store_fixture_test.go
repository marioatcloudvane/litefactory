package messaging

import (
	"context"
	"github.com/cloudvane/litefactory/internal/factory"
	"github.com/cloudvane/litefactory/internal/messagingstorage"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
	"time"
)

// fixturePersistence retains setup access only in the test process.
type fixturePersistence struct {
	messagingstorage.Port
	database *store.DB
}

// New composes legacy test fixtures through the same bounded production ports.
func New(db *store.DB, transport, owner string, channels []string, sender Transport, status StatusReader) *Service {
	persistence := fixturePersistence{Port: messagingstorage.New(db), database: db}
	return NewWithServices(persistence, factory.NewMessaging(db), transport, owner, channels, sender, status)
}

func fixtureDB(service *Service) *store.DB {
	persistence, ok := service.db.(fixturePersistence)
	if !ok {
		panic("messaging fixture database unavailable")
	}
	return persistence.database
}

// FactoryStatus composes the existing health projection in legacy test fixtures.
type FactoryStatus struct{ DB *store.DB }

// Read returns the factory-owned health projection.
func (f FactoryStatus) Read(ctx context.Context, at time.Time) (Status, error) {
	return factory.NewHealth(f.DB).Read(ctx, at)
}

func fixtureActions(service *Service) *owneraction.Service {
	actions, ok := service.ownerActions.(*owneraction.Service)
	if !ok {
		panic("messaging fixture actions unavailable")
	}
	return actions
}
