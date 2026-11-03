package store

import (
	"github.com/cloudvane/litefactory/internal/connectoraccess"
	"github.com/cloudvane/litefactory/internal/connectorstate"
	"github.com/cloudvane/litefactory/internal/factory/readtargets"
)

type accessTestStorage struct{ *DB }

func (s accessTestStorage) SameStore(other connectorstate.Storage) bool {
	p, ok := other.(accessTestStorage)
	return ok && p.DB == s.DB
}
func testConnectorAccess(db *DB) *connectoraccess.Service {
	return connectoraccess.New(accessTestStorage{db}, readtargets.New(db))
}
