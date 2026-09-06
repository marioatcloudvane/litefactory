// Package schedule parses Litefactory-owned scheduling controls.
//
// It has no clock, storage, forge, or agent dependency: callers supply the
// receipt time and default location so a parsed request is deterministic. A
// coding agent receives a schedule's Payload only after the factory has
// accepted and dispatched it.
package schedule
