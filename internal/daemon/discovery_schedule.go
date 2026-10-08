package daemon

import (
	"sync"
	"time"
)

// discoverySchedule decides, per repository, when the forge listing runs. The
// heartbeat tick runs all local work every time; a repository's listing (probe,
// issue listing, commented-issue listing) only when the schedule says it is due.
//
// A repository is due when any of these holds:
//   - no listing of it has finished since the daemon started;
//   - it is owed a listing: the last attempt failed, was held back by a
//     rate-limit backoff or deferred by the discovery budget, and no listing has
//     finished since;
//   - one interval has passed since its last finished listing.
//
// Webhook deliveries never move a repository's due time.
//
// State is in memory only: a restarted daemon lists every repository on its
// first heartbeat, which is what an unknown repository does.
type discoverySchedule struct {
	interval time.Duration

	mu       sync.Mutex
	repos    map[int64]*repoSchedule
	forceGen uint64
}

type repoSchedule struct {
	started     bool
	lastListing time.Time
	lastWebhook time.Time
	owed        bool
	// forced is the force generation this repository has listed since.
	forced uint64
	// announced is the bounded set of issue numbers that accepted webhook hints
	// named since the last finished listing; overflowed says the bound was hit
	// and the set is no longer complete.
	announced  map[int]struct{}
	overflowed bool
}

// maxAnnounced bounds the per-repository announced set.
const maxAnnounced = 1000

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

// newDiscoverySchedule returns a schedule whose first listing of every
// repository is due at once.
func newDiscoverySchedule(interval time.Duration) *discoverySchedule {
	return &discoverySchedule{interval: interval, repos: make(map[int64]*repoSchedule)}
}

func (s *discoverySchedule) state(repo int64) *repoSchedule {
	r := s.repos[repo]
	if r == nil {
		r = &repoSchedule{forced: s.forceGen}
		s.repos[repo] = r
	}
	return r
}

func (r *repoSchedule) anchor() time.Time {
	return r.lastListing
}

// Due reports whether repo's listing should run at now.
func (s *discoverySchedule) Due(repo int64, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.repos[repo]
	if r == nil || !r.started || r.owed || r.forced < s.forceGen {
		return true
	}
	return !now.Before(r.anchor().Add(s.interval))
}

// ListingFinished records that repo's listing completed at at. The owed flag and
// any force request are cleared: the next listing is due one interval after the
// last finished listing.
func (s *discoverySchedule) ListingFinished(repo int64, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.state(repo)
	r.started = true
	r.owed = false
	r.forced = s.forceGen
	r.announced, r.overflowed = nil, false
	if at.After(r.lastListing) {
		r.lastListing = at
	}
}

// ListingFailed marks repo as owed a listing: it stays due, whatever webhooks
// arrive, until a listing finishes. It is called for an attempt that failed, was
// backed off or deferred, and before an attempt that has not yet finished.
func (s *discoverySchedule) ListingFailed(repo int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state(repo).owed = true
}

// WebhookAccepted records a valid webhook delivery for repo at at without
// changing its listing deadline. Deliveries that queue no hint never reach it.
func (s *discoverySchedule) WebhookAccepted(repo int64, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.state(repo)
	if at.After(r.lastWebhook) {
		r.lastWebhook = at
	}
}

// Announce records that an accepted webhook hint named issue number for repo.
// The set is cleared when the repository's listing finishes. Beyond the bound
// the set is marked incomplete instead of growing.
func (s *discoverySchedule) Announce(repo int64, number int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.state(repo)
	if _, ok := r.announced[number]; ok {
		return
	}
	if len(r.announced) >= maxAnnounced {
		r.overflowed = true
		return
	}
	if r.announced == nil {
		r.announced = make(map[int]struct{})
	}
	r.announced[number] = struct{}{}
}

// Announced returns a copy of the issue numbers announced for repo since its
// last finished listing. complete is false when the bound was hit, so absence
// from the set proves nothing.
func (s *discoverySchedule) Announced(repo int64) (numbers map[int]struct{}, complete bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.repos[repo]
	if r == nil {
		return map[int]struct{}{}, true
	}
	numbers = make(map[int]struct{}, len(r.announced))
	for n := range r.announced {
		numbers[n] = struct{}{}
	}
	return numbers, !r.overflowed
}

// Forget drops the entry of a repository that is no longer registered.
func (s *discoverySchedule) Forget(repo int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.repos, repo)
}

// Known returns the repositories that have an entry.
func (s *discoverySchedule) Known() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]int64, 0, len(s.repos))
	for id := range s.repos {
		ids = append(ids, id)
	}
	return ids
}

// Snapshot returns repo's schedule; ok is false for a repository the schedule
// has not seen.
func (s *discoverySchedule) Snapshot(repo int64) (RepoSchedule, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.repos[repo]
	if r == nil {
		return RepoSchedule{}, false
	}
	snap := RepoSchedule{LastListing: r.lastListing, LastWebhook: r.lastWebhook, Owed: r.owed}
	if r.started {
		snap.NextDue = r.anchor().Add(s.interval)
	}
	return snap, true
}

// ForceDue makes every repository's next listing due at once, whatever the
// interval says, including repositories registered later. It stays due until
// that repository's listing finishes, so a tick that fails after the request
// does not lose it.
func (s *discoverySchedule) ForceDue() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forceGen++
}
