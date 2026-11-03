package contracts

import "time"

// RepoSchedule is a read-only snapshot of one repository's schedule.
type RepoSchedule struct {
	// LastListing is when the last listing finished; zero when none has since start.
	LastListing time.Time
	// LastWebhook is when the last valid webhook delivery was accepted; zero when none.
	LastWebhook time.Time
	// NextDue is when the countdown ends. Zero when the repository is due now
	// because no listing has finished yet.
	NextDue time.Time
	// Owed reports a listing attempt that has not finished.
	Owed bool
}
