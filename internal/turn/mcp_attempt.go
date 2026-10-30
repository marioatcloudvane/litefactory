package turn

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// AttemptAuthority is owned by one factory runtime, not by an MCP connection.
// Construct it once at startup; reconnects reuse a guard and never mint authority.
// Production lifecycle consumption is a separate integration step.
type AttemptAuthority struct {
	db       *store.DB
	instance string
	mu       sync.Mutex
	guards   map[string]*AttemptGuard
	closed   bool
}

// AttemptGuard binds the existing result launch to immutable permission scope.
// Cancellation signals revoke outstanding calls without waiting for their effects.
type AttemptGuard struct {
	owner  *AttemptAuthority
	lease  store.MCPLease
	ctx    context.Context
	cancel context.CancelFunc
}

// NewAttemptAuthority generates a fresh runtime identity that is never restored. Historical
// leases remain stored for reconciliation but cannot authenticate to this runtime.
func NewAttemptAuthority(ctx context.Context, db *store.DB) (*AttemptAuthority, error) {
	if db == nil {
		return nil, errors.New("MCP authority requires store")
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	instance := hex.EncodeToString(nonce[:])
	if err := db.StartMCPInstance(ctx, instance); err != nil {
		return nil, err
	}
	return &AttemptAuthority{db: db, instance: instance, guards: make(map[string]*AttemptGuard)}, nil
}

// Bind extends a factory-owned live result binding. Call for each actual launch,
// including resume/fallback, with its distinct existing attempt reference. The
// permission digest must cover the full immutable tool/target permission scope.
// The caller supplies the bounded attempt deadline; approval TTL is unrelated.
func (a *AttemptAuthority) Bind(ctx context.Context, b store.ResultBinding, permissionDigest string, expires time.Time) (*AttemptGuard, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil, store.ErrResultAuthority
	}
	lease := store.MCPLease{Binding: b, Instance: a.instance, PermissionDigest: permissionDigest, ExpiresAt: expires}
	if err := a.db.BindMCPLease(ctx, lease); err != nil {
		return nil, err
	}
	for key, old := range a.guards {
		if old.Binding().Task == b.Task {
			if err := a.db.CheckMCPLease(ctx, old.lease); err != nil {
				old.cancel()
				delete(a.guards, key)
			}
		}
	}
	// Expiry is immutable store authorization checked at every admission, not
	// an ambient turn deadline. Transport/runtime owners bound their own work;
	// explicit revocation or the supplied parent context cancels active calls.
	live, cancel := context.WithCancel(ctx)
	g := &AttemptGuard{owner: a, lease: lease, ctx: live, cancel: cancel}
	a.guards[attemptGuardKey(b)] = g
	return g, nil
}

// Binding returns a value copy, not mutable scope or a bearer capability.
func (g *AttemptGuard) Binding() store.ResultBinding { return g.lease.Binding }

// Lease returns a copy for trusted factory authorization primitives. It must not
// be accepted from MCP argument bytes as a replacement for the server's binding.
func (g *AttemptGuard) Lease() store.MCPLease { return g.lease }

// Check authenticates every operation, including discovery and historical replay.
func (g *AttemptGuard) Check(ctx context.Context) error {
	if err := g.ctx.Err(); err != nil {
		return store.ErrResultAuthority
	}
	return g.owner.db.CheckMCPLease(ctx, g.lease)
}

// Context joins request cancellation with attempt revocation and stamps the lease
// for transactional mutation/read admission in the existing shared result store.
// The caller must release the returned cancellation function after the operation.
func (g *AttemptGuard) Context(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if err := g.Check(ctx); err != nil {
		return nil, nil, err
	}
	live, cancel := context.WithCancel(ctx)
	// One bounded callback belongs to this call and is joined by release.
	done := make(chan struct{})
	stop := context.AfterFunc(g.ctx, func() { defer close(done); cancel() })
	release := func() {
		if !stop() {
			<-done
		}
		cancel()
	}
	if g.ctx.Err() != nil {
		release()
		return nil, nil, store.ErrResultAuthority
	}
	return store.WithMCPLease(live, g.lease), release, nil
}

// Revoke cancels outstanding contexts immediately and durably refuses subsequent
// admissions. Already recorded results remain available to internal recovery.
func (g *AttemptGuard) Revoke(ctx context.Context, reason string) error {
	if reason != "replaced" && reason != "cancelled" && reason != "closed" {
		return store.ErrResultAuthority
	}
	g.cancel()
	g.owner.mu.Lock()
	if g.owner.guards[attemptGuardKey(g.Binding())] == g {
		delete(g.owner.guards, attemptGuardKey(g.Binding()))
	}
	g.owner.mu.Unlock()
	return g.owner.db.RevokeMCPLease(ctx, g.lease, reason)
}

// Close invalidates and cancels every binding; it does not wait for remote work.
// Broker owners separately join their cancelled transport workers.
func (a *AttemptAuthority) Close(ctx context.Context) error {
	a.mu.Lock()
	a.closed = true
	guards := a.guards
	a.guards = make(map[string]*AttemptGuard)
	a.mu.Unlock()
	var errs []error
	for _, g := range guards {
		errs = append(errs, g.Revoke(ctx, "closed"))
	}
	return errors.Join(errs...)
}

func attemptGuardKey(b store.ResultBinding) string {
	return fmt.Sprintf("%s:%d:%s", b.Task, b.RunID, b.Attempt)
}
