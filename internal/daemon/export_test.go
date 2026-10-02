package daemon

// Seams for the external test package: the webhook handler's sink and the
// hint set are internal, and these let a test play a delivery without HTTP.

// WakeForTest queues a hint exactly as an accepted delivery does.
func (d *Daemon) WakeForTest(repoID int64, issue int) { d.webhook.Wake(repoID, issue) }

// PendingHintsForTest is how many hints are pending.
func (d *Daemon) PendingHintsForTest() int { return len(d.webhook.hints.pending()) }

// WakeSignalsForTest is how many wake signals are waiting on the channel.
func (d *Daemon) WakeSignalsForTest() int { return len(d.webhook.wake) }

// MaxPendingHintsForTest is the pending-hint bound.
const MaxPendingHintsForTest = maxPendingHints
