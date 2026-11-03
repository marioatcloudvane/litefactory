package discordcomponent

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/secrets"
)

func TestComponentCancellationDrainsActiveHandler(t *testing.T) {
	resolving := make(chan struct{})
	component, err := New(Options{Logger: slog.New(slog.DiscardHandler), Credentials: Credentials{Settings: config.DiscordChat{PublicKey: strings.Repeat("ab", 32)}, Resolve: func(context.Context) (secrets.Token, error) {
		close(resolving)
		return secrets.Token{}, errors.New("unavailable")
	}}})
	if err != nil {
		t.Fatal(err)
	}
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(canceled)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})
	component.handler.Store(&handler)
	run, cancel := context.WithCancel(t.Context())
	defer cancel()
	served := make(chan struct{})
	go func() { component.Serve(run); close(served) }()
	await := func(label string, ch <-chan struct{}) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for %s", label)
		}
	}
	component.Wake()
	await("serving lifetime", resolving)
	response := httptest.NewRecorder()
	handled := make(chan struct{})
	go func() {
		component.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/litefactory/discord", nil))
		close(handled)
	}()
	await("admitted handler", entered)
	cancel()
	await("request cancellation", canceled)
	deadline := time.Now().Add(5 * time.Second)
	for {
		component.admission.Lock()
		closed := component.closed
		component.admission.Unlock()
		if closed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("admission did not close")
		}
		time.Sleep(time.Millisecond)
	}
	refused := httptest.NewRecorder()
	component.Handler().ServeHTTP(refused, httptest.NewRequest(http.MethodPost, "/litefactory/discord", nil))
	if refused.Code != http.StatusServiceUnavailable {
		t.Fatalf("new request after stop: HTTP %d", refused.Code)
	}
	select {
	case <-served:
		t.Fatal("Serve returned while an admitted handler was still active")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	await("handler exit", handled)
	await("joined serving lifetime", served)
	if response.Code != http.StatusNoContent {
		t.Fatalf("active handler response: HTTP %d", response.Code)
	}
}
