package daemon

import (
	"errors"
	"slices"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestHintSetCollapsesDuplicatesAndBoundsItself(t *testing.T) {
	s := newHintSet(3)
	for range 10 {
		s.add(hintKey{repoID: 1, number: 5})
	}
	if got := s.pending(); len(got) != 1 {
		t.Fatalf("pending = %v, want one", got)
	}
	if s.takeOverflow() {
		t.Error("duplicates raised overflow")
	}

	s.add(hintKey{repoID: 1, number: 6})
	s.add(hintKey{repoID: 1, number: 7})
	s.add(hintKey{repoID: 1, number: 8}) // beyond the bound: dropped
	if got := s.pending(); len(got) != 3 || got[2] != (hintKey{repoID: 1, number: 7}) {
		t.Fatalf("pending = %v, want the first three in order", got)
	}
	if !s.takeOverflow() {
		t.Error("overflow not reported")
	}
	if s.takeOverflow() {
		t.Error("overflow reported twice")
	}

	s.resolve(hintKey{repoID: 1, number: 6})
	s.resolve(hintKey{repoID: 9, number: 9}) // unknown: no-op
	if got := s.pending(); len(got) != 2 {
		t.Errorf("pending = %v after resolve", got)
	}
}

func TestHintSetDropsAHintThatKeepsFailingAndAsksForAListing(t *testing.T) {
	s := newHintSet(10)
	k := hintKey{repoID: 1, number: 5}
	s.add(k)
	for i := 1; i < maxHintAttempts; i++ {
		if s.failed(k) {
			t.Fatalf("dropped after %d failures", i)
		}
	}
	if !s.failed(k) {
		t.Fatal("not dropped after the last attempt")
	}
	if len(s.pending()) != 0 || !s.takeOverflow() {
		t.Error("a dropped hint must leave the set and request a full listing")
	}
	if s.failed(k) {
		t.Error("failing an unknown hint reported a drop")
	}
}

func TestWebhookRuntimeWakeIsNonBlocking(t *testing.T) {
	w := newWebhookRuntime(WebhookOptions{}, nil)
	for range 5 {
		w.Wake(1, 5)
	}
	if len(w.wake) != 1 {
		t.Errorf("wake signals = %d, want 1", len(w.wake))
	}
}

func TestWebhookLookupIsCaseInsensitiveByKeyAndEmptyWithoutSecret(t *testing.T) {
	w := newWebhookRuntime(WebhookOptions{}, nil)
	w.repos[webhookKey("Acme", "API")] = &webhookRepo{id: 3, secret: []byte("x")}
	id, secret, found := w.Lookup("acme/api")
	if !found || id != 3 || string(secret) != "x" {
		t.Errorf("Lookup = %d, %q, %v", id, secret, found)
	}
	if _, _, found := w.Lookup("acme/other"); found {
		t.Error("an unknown repository was found")
	}
}

// exhaustDiscovery leaves background discovery with no way to read: its share is
// spent, real quota is under the floor (but above zero) and another operation
// heads the waiter queue.
func exhaustDiscovery(f *discoveryHTTPFixture) {
	b := &f.d.discovery
	b.mu.Lock()
	defer b.mu.Unlock()
	b.spent = b.discoveryShareLocked()
	b.remaining = b.floorLocked() - 500
	b.waiters = []discoveryWaiter{{key: "someone-else", cost: 1, seen: b.now}}
}

func hintFixture(t *testing.T) (*discoveryHTTPFixture, map[int64]*repoQueue, hintKey) {
	t.Helper()
	f := newDiscoveryHTTPFixture(t)
	repo, err := f.d.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "hinted", RemoteURL: "git@github.com:acme/hinted.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	f.d.webhook = &webhookRuntime{hints: newHintSet(10)}
	key := hintKey{repoID: repo.ID, number: 7}
	f.d.webhook.hints.add(key)
	byRepo := map[int64]*repoQueue{repo.ID: {repo: repo, forgeRepo: github.Repo{Owner: "acme", Name: "hinted"}, log: f.d.log, listing: true}}
	return f, byRepo, key
}

// A webhook-hinted issue is read through the operational lane, so neither the
// spent discovery share, the floor nor another head-of-line waiter holds it.
func TestWebhookHintReadIgnoresTheDiscoveryShare(t *testing.T) {
	f, byRepo, _ := hintFixture(t)
	exhaustDiscovery(f)
	ctx := f.d.discoveryContext(t.Context())
	if _, err := f.d.discoveryIssue(ctx, github.Repo{Owner: "acme", Name: "hinted"}, 8); !errors.Is(err, ErrDiscoveryDeferred) {
		t.Fatalf("background read under an exhausted budget: %v, want deferred", err)
	}
	seen := map[int64]*candidate{}
	result := &TickResult{}
	if err := f.d.webhookHints(ctx, byRepo, seen, result); err != nil {
		t.Fatal(err)
	}
	if result.Hinted != 1 || len(seen) != 1 || len(f.d.webhook.hints.pending()) != 0 {
		t.Fatalf("hinted=%d candidates=%d pending=%d, want 1, 1, 0\n%s", result.Hinted, len(seen), len(f.d.webhook.hints.pending()), f.logs.String())
	}
	for _, c := range seen {
		if !slices.Contains(c.sources, "webhook") || c.issue.Number != 7 {
			t.Fatalf("candidate = %+v", c)
		}
	}
}

// Without a token the hinted read still waits, and its hint stays pending.
func TestWebhookHintReadWaitsWithoutATokenOrQuota(t *testing.T) {
	for _, tc := range []struct {
		name  string
		empty func(b *discoveryBudget)
	}{
		{"no_token", func(b *discoveryBudget) { b.tokens = 0 }},
		{"no_quota", func(b *discoveryBudget) { b.remaining = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, byRepo, _ := hintFixture(t)
			exhaustDiscovery(f)
			f.d.discovery.mu.Lock()
			tc.empty(&f.d.discovery)
			f.d.discovery.mu.Unlock()
			seen := map[int64]*candidate{}
			result := &TickResult{}
			if err := f.d.webhookHints(f.d.discoveryContext(t.Context()), byRepo, seen, result); err != nil {
				t.Fatal(err)
			}
			if result.Hinted != 0 || len(seen) != 0 || len(f.d.webhook.hints.pending()) != 1 {
				t.Fatalf("hinted=%d candidates=%d pending=%d, want 0, 0, 1", result.Hinted, len(seen), len(f.d.webhook.hints.pending()))
			}
		})
	}
}

// A hinted task lists its comments through the operational lane; any other task
// still waits for background discovery.
func TestHintedTaskCommentsIgnoreTheDiscoveryShare(t *testing.T) {
	f := newDiscoveryHTTPFixture(t)
	exhaustDiscovery(f)
	ctx := f.d.discoveryContext(t.Context())
	repo := github.Repo{Owner: "acme", Name: "hinted"}
	if _, err := f.d.taskComments(ctx, &task{forgeRepo: repo, issue: github.Issue{Number: 7}}, 0); !errors.Is(err, ErrDiscoveryDeferred) {
		t.Fatalf("unhinted listing under an exhausted budget: %v, want deferred", err)
	}
	comments, err := f.d.taskComments(ctx, &task{forgeRepo: repo, issue: github.Issue{Number: 7}, hinted: true}, 0)
	if err != nil || len(comments) != 1 {
		t.Fatalf("hinted listing = %v, %v; want one comment", comments, err)
	}
}
