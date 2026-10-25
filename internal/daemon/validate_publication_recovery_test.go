package daemon_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
)

type validatePublicationObserver struct {
	daemon.Reporter
	calls int
	fail  bool
}

func (r *validatePublicationObserver) RecoverValidatePublications(context.Context, int64) error {
	r.calls++
	if r.fail {
		return errors.New("retained Validate index edit unavailable")
	}
	return nil
}

func TestValidatePublicationRecoveryUsesExistingTickWithoutOwnerReply(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("A retained Validate index")
	now := f.clock.Now()
	observed := &validatePublicationObserver{Reporter: f.reporter, fail: true}
	f.daemon = f.newDaemon(daemon.Options{Reporter: observed, Interval: time.Minute, Now: func() time.Time { return now }})
	f.settle()
	if observed.calls != 1 {
		t.Fatalf("automatic publication attempts=%d", observed.calls)
	}
	for range 3 {
		f.settle()
	}
	if observed.calls != 1 {
		t.Fatalf("ineligible tick retried publication: %d", observed.calls)
	}
	if len(f.turns.calls()) != 0 {
		t.Fatal("publication recovery invoked an agent")
	}
	now = now.Add(time.Minute)
	observed.fail = false
	f.settle()
	if observed.calls != 2 {
		t.Fatalf("due tick did not settle publication: %d", observed.calls)
	}
	if len(f.turns.calls()) != 0 {
		t.Fatal("publication settlement started work")
	}
}
