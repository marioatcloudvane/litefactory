package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/webhook"
)

// Webhook runtime limits.
const (
	// WebhookKeychainService is the keychain service every webhook secret is
	// stored under; the account is "<owner>/<name>".
	WebhookKeychainService = "litefactory-webhook"
	// webhookSecretRetry is the least time between two keychain lookups for a
	// repository whose secret is missing.
	webhookSecretRetry = 5 * time.Minute
	// maxPendingHints bounds the pending hint set. Beyond it hints are dropped
	// and the next pass lists the forge in full instead.
	maxPendingHints = 1000
	// maxHintAttempts is how many failed reads a hint survives before it is
	// replaced by a full listing.
	maxHintAttempts = 10
)

// WebhookOptions enable the webhook listener. The listener is already bound:
// [Open] binds it right after the singleton lock so that a taken port fails
// start-up before any work begins.
type WebhookOptions struct {
	Listener net.Listener
	// Secrets resolves per-repository secrets from the keychain. The zero
	// value uses the real keychain.
	Secrets secrets.Resolver
	// Now stamps accepted deliveries for the per-repository poll schedule; nil
	// means time.Now. It is separate from the daemon's tick clock, which only
	// Tick may read.
	Now func() time.Time
}

type hintKey struct {
	repoID int64
	number int
}

// hintSet is the bounded set of issues a webhook asked the daemon to re-read.
type hintSet struct {
	max int

	mu       sync.Mutex
	order    []hintKey
	attempts map[hintKey]int
	overflow bool
}

func newHintSet(limit int) *hintSet {
	return &hintSet{max: limit, attempts: make(map[hintKey]int)}
}

// add records a hint. A hint already pending collapses into the existing one;
// beyond the bound the hint is dropped and the overflow flag is raised.
func (s *hintSet) add(k hintKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.attempts[k]; ok {
		return
	}
	if len(s.order) >= s.max {
		s.overflow = true
		return
	}
	s.attempts[k] = 0
	s.order = append(s.order, k)
}

// pending is a snapshot of the hints, oldest first.
func (s *hintSet) pending() []hintKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]hintKey(nil), s.order...)
}

// resolve removes a hint that has been dealt with.
func (s *hintSet) resolve(k hintKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.attempts[k]; !ok {
		return
	}
	delete(s.attempts, k)
	for i, o := range s.order {
		if o == k {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
}

// failed counts a failed read and reports whether the hint ran out of
// attempts. A hint that did is removed and raises the overflow flag, so the
// next pass lists the forge in full.
func (s *hintSet) failed(k hintKey) bool {
	s.mu.Lock()
	n, ok := s.attempts[k]
	if !ok {
		s.mu.Unlock()
		return false
	}
	n++
	if n < maxHintAttempts {
		s.attempts[k] = n
		s.mu.Unlock()
		return false
	}
	s.overflow = true
	s.mu.Unlock()
	s.resolve(k)
	return true
}

// takeOverflow reports and clears the overflow flag.
func (s *hintSet) takeOverflow() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.overflow
	s.overflow = false
	return o
}

type webhookRepo struct {
	id      int64
	secret  []byte
	tried   bool
	lastTry time.Time
}

// webhookRuntime is the daemon side of the listener: the secret cache the
// handler reads, the hint set it writes and the wake channel the loop selects
// on. The handler never reaches the keychain; only [webhookRuntime.refresh],
// called at start-up and from the heartbeat, does.
type webhookRuntime struct {
	ln       net.Listener
	resolver secrets.Resolver
	log      *slog.Logger

	hints *hintSet
	// schedule is told of every accepted delivery so the repository's poll
	// countdown restarts; nil in unit tests that build the runtime alone.
	schedule *discoverySchedule
	now      func() time.Time
	// wake has one slot: a signal already waiting says everything a second one
	// would.
	wake chan struct{}

	mu    sync.RWMutex
	repos map[string]*webhookRepo
}

func newWebhookRuntime(opts WebhookOptions, log *slog.Logger) *webhookRuntime {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &webhookRuntime{
		now:      now,
		ln:       opts.Listener,
		resolver: opts.Secrets,
		log:      log,
		hints:    newHintSet(maxPendingHints),
		wake:     make(chan struct{}, 1),
		repos:    make(map[string]*webhookRepo),
	}
}

func webhookKey(owner, name string) string { return strings.ToLower(owner + "/" + name) }

// Lookup implements [webhook.Lookup] from the cache only.
func (w *webhookRuntime) Lookup(fullName string) (int64, []byte, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	r, ok := w.repos[fullName]
	if !ok {
		return 0, nil, false
	}
	return r.id, r.secret, true
}

// hasSecret reports whether a secret is cached for the repository key.
func (w *webhookRuntime) hasSecret(key string) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	r := w.repos[key]
	return r != nil && len(r.secret) > 0
}

// Wake implements [webhook.Sink]: queue the hint, signal without blocking.
func (w *webhookRuntime) Wake(repoID int64, issueNumber int) {
	w.hints.add(hintKey{repoID: repoID, number: issueNumber})
	if w.schedule != nil {
		w.schedule.WebhookAccepted(repoID, w.now())
		w.schedule.Announce(repoID, issueNumber)
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// refresh syncs the cache with the registered repositories and loads the
// secrets that are missing and due. A lookup that fails is retried no sooner
// than [webhookSecretRetry] later.
func (w *webhookRuntime) refresh(ctx context.Context, repos []store.Repo, now time.Time) {
	live := make(map[string]bool, len(repos))
	type due struct {
		key, account string
	}
	var loads []due

	w.mu.Lock()
	for _, repo := range repos {
		if repo.Forge != store.ForgeGitHub {
			continue
		}
		key := webhookKey(repo.Owner, repo.Name)
		live[key] = true
		entry := w.repos[key]
		if entry == nil {
			entry = &webhookRepo{}
			w.repos[key] = entry
		}
		entry.id = repo.ID
		if len(entry.secret) == 0 && (!entry.tried || now.Sub(entry.lastTry) >= webhookSecretRetry) {
			entry.tried, entry.lastTry = true, now
			loads = append(loads, due{key: key, account: repo.Owner + "/" + repo.Name})
		}
	}
	for key := range w.repos {
		if !live[key] {
			delete(w.repos, key)
		}
	}
	w.mu.Unlock()

	for _, load := range loads {
		if ctx.Err() != nil {
			return
		}
		token, err := w.resolver.Resolve(ctx, secrets.KeychainRef(WebhookKeychainService, load.account))
		if err != nil {
			w.log.DebugContext(ctx, "no webhook secret loaded for a repository; deliveries for it are refused until one is stored",
				slog.String("repo", load.account), slog.Any("error", err))
			continue
		}
		w.mu.Lock()
		if entry := w.repos[load.key]; entry != nil {
			entry.secret = []byte(token.Reveal())
		}
		w.mu.Unlock()
	}
}

// webhookServe runs the HTTP server until ctx is cancelled, then shuts it down
// gracefully within grace.
func (d *Daemon) webhookServe(ctx context.Context, wg *sync.WaitGroup) error {
	handler, err := webhook.NewHandler(webhook.Options{Lookup: d.webhook, Sink: d.webhook, Logger: d.log.With(slog.String("component", "webhook"))})
	if err != nil {
		return err
	}
	server := webhook.NewServer(d.webhook.ln, handler)
	d.log.InfoContext(ctx, "webhook listener serving", slog.String("addr", d.webhook.ln.Addr().String()), slog.String("path", webhook.Path))
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := server.Serve(); err != nil {
			d.log.ErrorContext(ctx, "webhook listener stopped unexpectedly", slog.Any("error", err))
		}
	}()
	go func() {
		defer wg.Done()
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), d.grace)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			d.log.Warn("webhook listener did not drain within the grace period; closed", slog.Any("error", err))
		}
	}()
	return nil
}

// closeWebhook closes the listener. It is safe after a graceful shutdown.
func (d *Daemon) closeWebhook() error {
	if d.webhook == nil {
		return nil
	}
	if err := d.webhook.ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("close webhook listener: %w", err)
	}
	return nil
}

// refreshWebhookSecrets keeps the secret cache current from the heartbeat.
func (d *Daemon) refreshWebhookSecrets(ctx context.Context, now time.Time) {
	if d.webhook == nil {
		return
	}
	repos, err := d.db.ListRepos(ctx)
	if err != nil {
		d.log.WarnContext(ctx, "webhook secrets not refreshed; the repository list could not be read", slog.Any("error", err))
		return
	}
	d.webhook.refresh(ctx, repos, now)
}

// webhookHints turns pending hints into candidates beside the other discovery
// sources. Each hinted issue is read fresh through the operational lane
// ([doctorCall]); the webhook payload was never state. A read that fails or is
// deferred leaves its hint pending for the next heartbeat.
func (d *Daemon) webhookHints(ctx context.Context, byRepo map[int64]*repoQueue, seen map[int64]*candidate, result *TickResult) error {
	if d.webhook == nil {
		return nil
	}
	probed := make(map[int64]bool)
	for _, hint := range d.webhook.hints.pending() {
		if err := ctx.Err(); err != nil {
			return err
		}
		q := byRepo[hint.repoID]
		if q == nil {
			// The repository is gone or not pollable; there is nothing to wake.
			d.webhook.hints.resolve(hint)
			continue
		}
		if !q.listing && !probed[hint.repoID] {
			// Background reads wait for a fresh quota observation, which a
			// listing tick gets from its probe and a heartbeat-only tick must
			// ask for.
			probed[hint.repoID] = true
			if err := d.discoveryProbe(ctx, q.forgeRepo); err != nil {
				return err
			}
		}
		marked, err := d.markCommentedChild(ctx, q.log, q, hint.number)
		if err != nil {
			return err
		}
		if marked {
			d.webhook.hints.resolve(hint)
			continue
		}
		// An owner's comment must not wait behind background discovery, so the
		// hinted read takes the operational lane: a token and real quota left,
		// not the discovery share, floor or head-of-line queue.
		issue, err := doctorCall(ctx, d, func(ctx context.Context) (github.Issue, error) {
			return d.forge.GetIssue(ctx, q.forgeRepo, hint.number)
		})
		switch {
		case errors.Is(err, ErrDiscoveryDeferred):
			continue
		case errors.Is(err, github.ErrNotFound), errors.Is(err, github.ErrInvalidArgument):
			// Deleted, transferred or a pull request: not work for this daemon.
			d.webhook.hints.resolve(hint)
			continue
		case err != nil:
			var rate *github.RateLimitError
			if errors.As(err, &rate) || ctx.Err() != nil {
				return fmt.Errorf("read %s#%d, named by a webhook: %w", q.forgeRepo, hint.number, err)
			}
			q.log.WarnContext(ctx, "reading a webhook-hinted issue failed; it stays pending",
				slog.Int("issue", hint.number), slog.Any("error", err))
			if d.webhook.hints.failed(hint) {
				q.log.WarnContext(ctx, "a webhook hint kept failing and was dropped; the next pass lists the forge in full",
					slog.Int("issue", hint.number))
			}
			continue
		}
		c, err := d.sight(ctx, q, issue)
		if err != nil {
			return err
		}
		d.webhook.hints.resolve(hint)
		result.Hinted++
		if c == nil {
			continue
		}
		if existing := seen[c.session.ID]; existing != nil {
			existing.addSource("webhook")
			continue
		}
		c.addSource("webhook")
		seen[c.session.ID] = c
		q.candidates = append(q.candidates, c)
	}
	return nil
}
