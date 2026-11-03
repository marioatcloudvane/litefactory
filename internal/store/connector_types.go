package store

import (
	"time"

	"github.com/cloudvane/litefactory/internal/connectorstate"
)

// ConnectorOwner retains the connector-owned compatibility value.
// Compatibility names preserve serialized records; store owns atomic persistence only.
type ConnectorOwner = connectorstate.ConnectorOwner

// ConnectorClient retains the connector-owned compatibility value.
type ConnectorClient = connectorstate.ConnectorClient

// ConnectorBrowser retains the connector-owned compatibility value.
type ConnectorBrowser = connectorstate.ConnectorBrowser

// ConnectorGrantTarget retains the connector-owned compatibility value.
type ConnectorGrantTarget = connectorstate.ConnectorGrantTarget

// ConnectorUsageTarget retains the connector-owned compatibility value.
type ConnectorUsageTarget = connectorstate.ConnectorUsageTarget

// ConnectorRepository retains the connector-owned compatibility value.
type ConnectorRepository = connectorstate.ConnectorRepository

// ConnectorCoverage retains the connector-owned compatibility value.
type ConnectorCoverage = connectorstate.ConnectorCoverage

// ConnectorGrant retains the connector-owned compatibility value.
type ConnectorGrant = connectorstate.ConnectorGrant

// ConnectorRefusal retains the connector-owned compatibility value.
type ConnectorRefusal = connectorstate.ConnectorRefusal

// ConnectorCode retains the connector-owned compatibility value.
type ConnectorCode = connectorstate.ConnectorCode

// ConnectorTokenPair retains the connector-owned compatibility value.
type ConnectorTokenPair = connectorstate.ConnectorTokenPair

// ConnectorExchange retains the connector-owned compatibility value.
type ConnectorExchange = connectorstate.ConnectorExchange

// ConnectorGrantSummary retains the connector-owned compatibility value.
type ConnectorGrantSummary = connectorstate.ConnectorGrantSummary

// ErrConnectorScope retains the connector-owned compatibility value.
var ErrConnectorScope = connectorstate.ErrConnectorScope

// ErrConnectorInvalid retains the connector-owned compatibility value.
var ErrConnectorInvalid = connectorstate.ErrConnectorInvalid

// ErrConnectorCapacity retains the connector-owned compatibility value.
var ErrConnectorCapacity = connectorstate.ErrConnectorCapacity

// ErrConnectorOwner retains the connector-owned compatibility value.
var ErrConnectorOwner = connectorstate.ErrConnectorOwner

// ConnectorMaxGrantMonths retains the connector-owned compatibility value.
const ConnectorMaxGrantMonths = connectorstate.ConnectorMaxGrantMonths

func connectorRefused(kind error, format string, args ...any) error {
	return connectorstate.Refused(kind, format, args...)
}
func connectorRefusedCode(kind error, code, format string, args ...any) error {
	return connectorstate.RefusedCode(kind, code, format, args...)
}
func connectorScopeKnown(scope string) bool { return connectorstate.ScopeKnown(scope) }
func connectorScopeMatches(requested, granted string) bool {
	return connectorstate.ScopeMatches(requested, granted)
}

// ConnectorRefusalCode delegates to the connector-owned value contract.
func ConnectorRefusalCode(err error) string { return connectorstate.ConnectorRefusalCode(err) }

// ConnectorRefusalReason delegates to the connector-owned value contract.
func ConnectorRefusalReason(err error) string { return connectorstate.ConnectorRefusalReason(err) }

// ConnectorRepositoryScoped delegates to the connector-owned value contract.
func ConnectorRepositoryScoped(scope string) bool {
	return connectorstate.ConnectorRepositoryScoped(scope)
}

// ConnectorGrantExpiry delegates to the connector-owned value contract.
func ConnectorGrantExpiry(now time.Time, months int) time.Time {
	return connectorstate.ConnectorGrantExpiry(now, months)
}

// ConnectorMaxGrantExpiry delegates to the connector-owned value contract.
func ConnectorMaxGrantExpiry(now time.Time) time.Time {
	return connectorstate.ConnectorMaxGrantExpiry(now)
}

// ConnectorScopes delegates to the connector-owned value contract.
func ConnectorScopes(scope string) ([]string, bool) { return connectorstate.ConnectorScopes(scope) }

// ConnectorScopeHas delegates to the connector-owned value contract.
func ConnectorScopeHas(scope, token string) bool {
	return connectorstate.ConnectorScopeHas(scope, token)
}

// ConnectorScopeSubset delegates to the connector-owned value contract.
func ConnectorScopeSubset(sub, super string) bool {
	return connectorstate.ConnectorScopeSubset(sub, super)
}

// ConnectorKnownScopes delegates to the connector-owned value contract.
func ConnectorKnownScopes() []string { return connectorstate.ConnectorKnownScopes() }

var connectorRepositoryScopes = connectorstate.RepositoryScopes()
