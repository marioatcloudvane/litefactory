// Package meter provides deterministic token accounting without database,
// subprocess, network or clock ownership. Callers persist accepted source
// identities, checkpoints and measurements in one transaction. Inclusive
// evidence is retained for reconciliation and never added to exclusive totals.
package meter
