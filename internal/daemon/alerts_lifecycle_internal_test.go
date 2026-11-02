package daemon

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/alerts"
)

type cancelAlertTransport struct{ entered chan struct{} }

func (t cancelAlertTransport) SendAlert(ctx context.Context, _ alerts.Message) alerts.Delivery {
	select {
	case t.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return alerts.Delivery{Unavailable: "cancelled"}
}
func (cancelAlertTransport) ReconcileAlert(context.Context, alerts.Message, string) (string, error) {
	return "", nil
}

func TestFactoryAlertMonitorContinuesWithoutSlotsAndJoinsCanceledSend(t *testing.T) {
	t.Parallel()
	d, _, at := alertDaemonFixture(t)
	d.alertClock = func() time.Time { return at }
	d.alertChannel, d.alertOwner = "3", "7"
	transport := cancelAlertTransport{entered: make(chan struct{}, 1)}
	d.alertSender = transport
	if !d.pool.acquire(1) {
		t.Fatal("fill pool")
	}
	defer d.pool.release(1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var joined sync.WaitGroup
	d.serveAlerts(ctx, &joined)
	select {
	case <-transport.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("alert sender did not run while slots were full")
	}
	cancel()
	done := make(chan struct{})
	go func() { joined.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("alert monitor did not join its canceled sender")
	}
	used, total := d.pool.capacity()
	if used != 1 || total != 1 {
		t.Fatalf("monitor changed execution capacity: %d/%d", used, total)
	}
}
