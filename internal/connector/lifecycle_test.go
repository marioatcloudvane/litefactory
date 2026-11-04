package connector

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/connectorstate"
)

type lifecycleStorage struct {
	connectorstate.Storage
	reconciliations atomic.Int32
}

func (s *lifecycleStorage) SameStore(other connectorstate.Storage) bool {
	if wrapped, ok := other.(*lifecycleStorage); ok {
		other = wrapped.Storage
	}
	return s.Storage.SameStore(other)
}

func (s *lifecycleStorage) ReconcileConnectorOwner(ctx context.Context, pin int64, at time.Time) (connectorstate.ConnectorOwner, error) {
	s.reconciliations.Add(1)
	return s.Storage.ReconcileConnectorOwner(ctx, pin, at)
}

func TestPrepareReconcilesBeforeReadinessAndSecrets(t *testing.T) {
	t.Parallel()
	f := newConnectorFixture(t, nil)
	storage := &lifecycleStorage{Storage: f.options.Storage}
	f.options.Storage = storage
	local, err := NewLocalControl(t.Context(), storage, f.control)
	if err != nil {
		t.Fatal(err)
	}
	f.options.LocalControl = local
	f.control.unavailable.Store(true)
	var resolved atomic.Bool
	runtime, err := Prepare(t.Context(), LifecycleOptions{Options: f.options, ResolveSecret: func(context.Context, config.ConnectorSecretRef) (string, error) {
		resolved.Store(true)
		return "", errors.New("unavailable")
	}})
	if runtime != nil || err == nil {
		t.Fatal("unready local control accepted")
	}
	if !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("wrong readiness refusal: %v", err)
	}
	if storage.reconciliations.Load() != 1 {
		t.Fatal("owner was not reconciled before readiness failed")
	}
	if resolved.Load() {
		t.Fatal("secrets resolved before readiness")
	}
}

func TestPrepareDisabledNeedsNoDependencies(t *testing.T) {
	t.Parallel()
	runtime, err := Prepare(t.Context(), LifecycleOptions{})
	if runtime != nil || err != nil {
		t.Fatalf("disabled preparation: %v", err)
	}
}

func TestRuntimeCloseReleasesPreparedListenerOnce(t *testing.T) {
	t.Parallel()
	f := newConnectorFixture(t, nil)
	var lc net.ListenConfig
	reservation, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f.options.Config.ListenAddr = reservation.Addr().String()
	if err := reservation.Close(); err != nil {
		t.Fatal(err)
	}
	runtime, err := Prepare(t.Context(), LifecycleOptions{Options: f.options, ResolveSecret: func(_ context.Context, ref config.ConnectorSecretRef) (string, error) {
		if ref.Env == f.options.Config.CookieSecretRef.Env {
			return base64.StdEncoding.EncodeToString(f.options.CookieKey), nil
		}
		return "dedicated-secret", nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	var done sync.WaitGroup
	// Four close calls terminate when sync.Once finishes releasing the listener.
	for range 4 {
		done.Go(func() {
			if err := runtime.Close(); err != nil {
				t.Errorf("close: %v", err)
			}
		})
	}
	done.Wait()
	rebound, err := lc.Listen(t.Context(), "tcp", runtime.Addr().String())
	if err != nil {
		t.Fatalf("prepared listener leaked: %v", err)
	}
	if err := rebound.Close(); err != nil {
		t.Fatal(err)
	}
}
