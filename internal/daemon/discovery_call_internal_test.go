package daemon

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/secrets"
)

type discoveryUnitTransport func(*http.Request) (*http.Response, error)

func (f discoveryUnitTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// These tests use an in-memory transport: no server, socket, store or clock sleep.
func TestDiscoveryCall_OverlappingHistories(t *testing.T) {
	for _, cancelWaiting := range []bool{false, true} {
		name := "short_history_refund_admits_waiter"
		if cancelWaiting {
			name = "cancel_waiter_preserves_inflight_reservation"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				d := &Daemon{}
				now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
				d.discovery.tick(now)
				d.discovery.observe(github.RequestObservation{CoreRateValid: true, Limit: 5000, Remaining: 5000, Reset: now.Add(time.Hour), StatusCode: 200})
				release := make(chan struct{})
				started := make(chan struct{})
				client, err := github.New(github.Options{
					Token: secrets.NewToken("offline-unit-token"), Observe: d.discovery.observe,
					HTTPClient: &http.Client{Transport: discoveryUnitTransport(func(r *http.Request) (*http.Response, error) {
						if strings.Contains(r.URL.Path, "/issues/1/comments") {
							close(started)
							select {
							case <-release:
							case <-r.Context().Done():
								return nil, r.Context().Err()
							}
						}
						body := `[{"id":701,"body":"owner work"}]`
						if strings.HasSuffix(r.URL.Path, "/issues/3") {
							body = `{"number":3,"state":"open"}`
						}
						return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
					})},
				})
				if err != nil {
					t.Fatal(err)
				}
				d.forge = client
				repo := github.Repo{Owner: "org", Name: "repo"}
				type result struct {
					comments []github.Comment
					err      error
				}
				first := make(chan result, 1)
				go func() { comments, err := d.discoveryComments(t.Context(), repo, 1, 0); first <- result{comments, err} }()
				synctest.Wait()
				select {
				case <-started:
				default:
					t.Fatal("first history did not reach transport")
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				second := make(chan result, 1)
				go func() { comments, err := d.discoveryComments(ctx, repo, 2, 0); second <- result{comments, err} }()
				synctest.Wait()
				select {
				case got := <-second:
					t.Fatalf("overlapping history returned before reservation refund: %+v", got)
				default:
				}

				// Foreground execution traffic must return even while discovery holds its
				// full list reservation; it only updates the observed quota.
				issue, err := client.GetIssue(t.Context(), repo, 3)
				if err != nil || issue.Number != 3 {
					t.Fatalf("foreground issue = %+v, %v", issue, err)
				}
				if cancelWaiting {
					cancel()
					synctest.Wait()
					select {
					case got := <-second:
						if !errors.Is(got.err, context.Canceled) || got.comments != nil {
							t.Fatalf("cancelled waiter = %+v", got)
						}
					default:
						t.Fatal("cancelled waiter did not terminate")
					}
					if d.discovery.reserved != 100 || d.discovery.tokens != float64(discoveryBurst-100) {
						t.Fatalf("cancel affected first reservation: reserved=%d tokens=%v", d.discovery.reserved, d.discovery.tokens)
					}
				}
				close(release)
				synctest.Wait()
				select {
				case got := <-first:
					if got.err != nil || len(got.comments) != 1 || got.comments[0].ID != 701 {
						t.Fatalf("first history = %+v", got)
					}
				default:
					t.Fatal("released first history did not terminate")
				}
				if !cancelWaiting {
					select {
					case got := <-second:
						if got.err != nil || len(got.comments) != 1 || got.comments[0].ID != 701 {
							t.Fatalf("second history = %+v", got)
						}
					default:
						t.Fatal("second history did not resume after refund")
					}
				}
				wantRequests := 2
				if cancelWaiting {
					wantRequests = 1
				}
				if d.discovery.reserved != 0 || d.discovery.tokens != float64(discoveryBurst-wantRequests) || d.discovery.spent != wantRequests || d.discovery.deferrals != 0 {
					t.Fatalf("settlement: reserved=%d tokens=%v spent=%d deferrals=%d; want 0, %d, %d, 0", d.discovery.reserved, d.discovery.tokens, d.discovery.spent, d.discovery.deferrals, discoveryBurst-wantRequests, wantRequests)
				}
			})
		})
	}
}
