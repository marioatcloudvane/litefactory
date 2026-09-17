package turn

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func TestMCPAttempt_PrivateSocketRevocationAndRecovery(t *testing.T) {
	for _, state := range []string{"active", "replaced", "cancelled", "closed", "restart"} {
		t.Run(state, func(t *testing.T) {
			db, session, o := agentResultFixture(t, "research")
			service, err := BindAgentResultService(t.Context(), o)
			if err != nil {
				t.Fatal(err)
			}
			authority, err := NewAttemptAuthority(t.Context(), db)
			if err != nil {
				t.Fatal(err)
			}
			guard, err := authority.Bind(t.Context(), service.Binding(), strings.Repeat("a", 64), time.Now().Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = authority.Close(context.Background()) })
			dir, err := os.MkdirTemp("/tmp", "mcp-lease-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			dir, err = filepath.EvalSymlinks(dir)
			if err != nil {
				t.Fatal(err)
			}
			broker, err := submission.Start(t.Context(), submission.Options{Directory: dir, AuthorizedStories: []string{"US-001"}, MaxWorkers: 2, RequestTimeout: time.Second, Handler: func(ctx context.Context, q submission.Request) (json.RawMessage, error) {
				live, release, err := guard.Context(ctx)
				if err != nil {
					return nil, err
				}
				defer release()
				switch q.Method {
				case "preflight":
					err = db.CheckResultWriter(live, guard.Binding())
					return json.RawMessage(`{"valid":true}`), err
				case "submit":
					r, err := service.Submit(live, q.RequestID, resultCreate("research"))
					if err != nil {
						return nil, err
					}
					return json.Marshal(r)
				case "receipt":
					r, err := db.GetResultReceipt(live, guard.Binding(), guard.Binding().RunID, q.RequestID)
					if err != nil {
						return nil, err
					}
					return json.Marshal(r)
				}
				return nil, errors.New("unsupported")
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer broker.Close()
			payload := json.RawMessage(`{"stage":"engineering","story":"US-001"}`)
			first, err := submission.Call(t.Context(), broker.DescriptorPath(), "submit", payload)
			if err != nil || first.Error != "" {
				t.Fatalf("first submit: %+v %v", first, err)
			}
			outstanding, release, err := guard.Context(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			switch state {
			case "replaced":
				b, err := db.BindResultAttempt(t.Context(), agentResultRun(t, db, session, "research", "resume", 2))
				if err != nil {
					t.Fatal(err)
				}
				next, err := authority.Bind(t.Context(), b, strings.Repeat("a", 64), time.Now().Add(time.Hour))
				if err != nil {
					t.Fatal(err)
				}
				if next.Binding().Attempt == guard.Binding().Attempt {
					t.Fatal("reused attempt")
				}
			case "cancelled", "closed":
				if err := guard.Revoke(t.Context(), state); err != nil {
					t.Fatal(err)
				}
			case "restart":
				restarted, err := NewAttemptAuthority(t.Context(), db)
				if err != nil {
					t.Fatal(err)
				}
				defer restarted.Close(context.Background())
			}
			if state == "replaced" || state == "cancelled" || state == "closed" {
				select {
				case <-outstanding.Done():
				case <-time.After(time.Second):
					t.Fatal("revocation did not cancel outstanding work")
				}
			}
			for _, method := range []string{"preflight", "submit", "receipt"} {
				data := payload
				if method == "receipt" {
					data, _ = json.Marshal(map[string]string{"request_id": first.RequestID})
				}
				r, err := submission.Call(t.Context(), broker.DescriptorPath(), method, data)
				if err != nil {
					t.Fatal(err)
				}
				if state == "active" {
					if r.Error != "" {
						t.Fatal(r.Error)
					}
				} else if r.Error == "" {
					t.Fatalf("%s accepted %s authority", method, state)
				}
			}
			if _, err := db.GetResultReceipt(t.Context(), guard.Binding(), guard.Binding().RunID, first.RequestID); err != nil {
				t.Fatalf("internal reconciliation lost: %v", err)
			}
		})
	}
}

func TestMCPAttempt_RevokeBeforeAdmissionAndAfterRecordedWork(t *testing.T) {
	for _, admit := range []bool{false, true} {
		t.Run(map[bool]string{false: "before", true: "after"}[admit], func(t *testing.T) {
			db, _, o := agentResultFixture(t, "research")
			service, err := BindAgentResultService(t.Context(), o)
			if err != nil {
				t.Fatal(err)
			}
			authority, err := NewAttemptAuthority(t.Context(), db)
			if err != nil {
				t.Fatal(err)
			}
			defer authority.Close(context.Background())
			guard, err := authority.Bind(t.Context(), service.Binding(), strings.Repeat("a", 64), time.Now().Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			live, release, err := guard.Context(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			ready := make(chan struct{})
			finish := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				if admit {
					_, err := service.Submit(live, "admission", resultCreate("research"))
					if err != nil {
						done <- err
						close(ready)
						return
					}
				}
				close(ready)
				<-finish
				if !admit {
					_, err := service.Submit(live, "admission", resultCreate("research"))
					done <- err
					return
				}
				done <- nil
			}()
			<-ready
			if err := guard.Revoke(t.Context(), "cancelled"); err != nil {
				t.Fatal(err)
			}
			select {
			case <-live.Done():
			case <-time.After(time.Second):
				t.Fatal("slow external effect blocked revocation")
			}
			close(finish)
			err = <-done
			if !admit && err == nil {
				t.Fatal("revoked admission succeeded")
			}
			if admit && err != nil {
				t.Fatal(err)
			}
			_, err = db.GetResultReceipt(t.Context(), guard.Binding(), guard.Binding().RunID, "admission")
			if admit && err != nil {
				t.Fatalf("admitted work lost: %v", err)
			}
			if !admit && !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("pre-admission cancellation left work: %v", err)
			}
		})
	}
}

func TestMCPAttempt_FreshLaunchBindingsAndCopiedScope(t *testing.T) {
	db, session, o := agentResultFixture(t, "research")
	authority, err := NewAttemptAuthority(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	defer authority.Close(context.Background())
	var previous *AttemptGuard
	seen := map[string]bool{}
	for i, launch := range []string{"cold", "resume", "failed-resume-fallback", "cold-fallback"} {
		a := o.Attempt
		if i > 0 {
			a = agentResultRun(t, db, session, "research", launch, int64(i+1))
		}
		b, err := db.BindResultAttempt(t.Context(), a)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := authority.Bind(t.Context(), b, "invalid", time.Now().Add(time.Hour)); err == nil {
			t.Fatal("invalid permission digest accepted")
		}
		g, err := authority.Bind(t.Context(), b, strings.Repeat("a", 64), time.Now().Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if seen[g.Binding().Attempt] {
			t.Fatal("launch reused authority")
		}
		seen[g.Binding().Attempt] = true
		if previous != nil {
			if err := previous.Check(t.Context()); !errors.Is(err, store.ErrResultAuthority) {
				t.Fatalf("old launch live: %v", err)
			}
		}
		forged := g.Lease()
		forged.Binding.Task = "github:foreign/repo#1"
		forged.PermissionDigest = strings.Repeat("f", 64)
		if err := db.CheckMCPLease(t.Context(), forged); !errors.Is(err, store.ErrResultAuthority) {
			t.Fatalf("forged copied scope accepted: %v", err)
		}
		if err := g.Check(t.Context()); err != nil {
			t.Fatalf("copy mutated live scope: %v", err)
		}
		if _, err := authority.Bind(t.Context(), b, strings.Repeat("a", 64), time.Now().Add(time.Hour)); err == nil {
			t.Fatal("same launch reminted authority")
		}
		if err := g.Check(t.Context()); err != nil {
			t.Fatalf("failed rebind revoked original: %v", err)
		}
		previous = g
	}
}
