package daemon

import (
	"context"
	"errors"
	"testing"
)

func TestDoctorDrainCancelsOnlySelectedWriterAndJoins(t *testing.T) {
	var f inflight
	if !f.enter(406) || !f.enter(407) {
		t.Fatal("admission")
	}
	selected, cancel := context.WithCancel(t.Context())
	defer cancel()
	sibling, cancelSibling := context.WithCancel(t.Context())
	defer cancelSibling()
	f.bindCancel(406, cancel)
	f.bindCancel(407, cancelSibling)
	completed := make(chan error, 1)
	go func() { completed <- f.drain(t.Context(), 406, true) }()
	<-selected.Done()
	select {
	case err := <-completed:
		t.Fatal("drain returned before writer settled", err)
	default:
	}
	if sibling.Err() != nil {
		t.Fatal("sibling cancelled")
	}
	f.leave(406)
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(t.Context())
	stop()
	if err := f.drain(ctx, 407, false); !errors.Is(err, context.Canceled) {
		t.Fatal("uncertain parent writer was not refused", err)
	}
	if sibling.Err() != nil {
		t.Fatal("parent join cancelled unrelated work")
	}
	f.leave(407)
}
