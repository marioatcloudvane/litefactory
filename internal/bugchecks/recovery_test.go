package bugchecks

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"testing"
)

func TestBugCheckRecoveryProcessObserverCancellationAndUnsupportedHost(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if stopped, err := ProcessStopped(ctx, "{}"); stopped || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled observer: %v %v", stopped, err)
	}
	if runtime.GOOS != "linux" {
		if stopped, err := ProcessStopped(t.Context(), "{}"); stopped || err == nil {
			t.Fatalf("unsupported host certified cleanup: %v %v", stopped, err)
		}
	}
}
func TestBugCheckRecoveryLinuxLiveNamespaceCannotCertifyStopped(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux /proc observation unavailable; no actual host namespace proof")
	}
	identity, err := captureProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if stopped, err := ProcessStopped(t.Context(), identity); err != nil || stopped {
		t.Fatalf("live process certified stopped: %v %v", stopped, err)
	}
	var changed processIdentity
	if err = json.Unmarshal([]byte(identity), &changed); err != nil {
		t.Fatal(err)
	}
	changed.Start = "0" // Model a reused PID: the original namespace still has live members.
	raw, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	if stopped, err := ProcessStopped(t.Context(), string(raw)); err != nil || stopped {
		t.Fatalf("start mismatch certified surviving namespace stopped: %v %v", stopped, err)
	}
}
