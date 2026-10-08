// Package testutil contains test-only timing bounds.
package testutil

import "time"

// Wait bounds real-time rendezvous and cleanup. It detects hangs, not speed.
const Wait = 30 * time.Second
