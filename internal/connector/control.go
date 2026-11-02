// Package connector provides the owner-only remote read boundary. It never
// receives attempt execution or factory mutation capabilities.
package connector

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/connectorstate"
)

// GrantControl is the local owner control surface supplied by trusted daemon
// composition. The command implementation belongs to the local-control story.
type GrantControl interface {
	Ready(context.Context) error
	List(context.Context) ([]connectorstate.ConnectorGrantSummary, error)
	Revoke(context.Context, string) error
	RevokeAll(context.Context) error
}

// LocalControl binds a working local command host to the same durable database.
// It cannot be enabled through operator configuration or remote routes.
type LocalControl struct {
	storage connectorstate.Storage
	control GrantControl
}

// NewLocalControl registers the factory-owned local command implementation.
// Ready must verify its actual command host, not an operator enablement flag.
func NewLocalControl(ctx context.Context, storage connectorstate.Storage, control GrantControl) (*LocalControl, error) {
	if storage == nil || control == nil {
		return nil, errors.New("connector local grant control is unavailable")
	}
	if err := control.Ready(ctx); err != nil {
		return nil, errors.New("connector local grant control is not ready")
	}
	if _, err := control.List(ctx); err != nil {
		return nil, errors.New("connector local grant control cannot read grants")
	}
	return &LocalControl{storage: storage, control: control}, nil
}

func (c *LocalControl) check(ctx context.Context, storage connectorstate.Storage, _ int64) error {
	if c == nil || c.storage == nil || !c.storage.SameStore(storage) || c.control == nil {
		return errors.New("connector requires local grant control")
	}
	if err := c.control.Ready(ctx); err != nil {
		return errors.New("connector local grant control is not ready")
	}
	return nil
}
