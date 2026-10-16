package providerprocess

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestProbeTransientRecoveryAndBounds(t *testing.T) {
	for _, failure := range []string{"kill -KILL $$", "exec /bin/sleep 30", "exit 7"} {
		for _, recoverProbe := range []bool{false, true} {
			t.Run(failure+string(rune('0'+boolInt(recoverProbe))), func(t *testing.T) {
				dir := t.TempDir()
				binary := filepath.Join(dir, "probe")
				script := "#!/bin/sh\necho attempt >> '" + filepath.Join(dir, "attempts") + "'\n" + failure + "\n"
				if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
				var waits []time.Duration
				wait := func(_ context.Context, d time.Duration) error {
					waits = append(waits, d)
					if recoverProbe {
						return os.WriteFile(binary, []byte("#!/bin/sh\necho capabilities\n"), 0700)
					}
					return nil
				}
				output, err := conversationProbe(t.Context(), binary, nil, []string{}, dir, 2*time.Second, wait)
				switch {
				case failure == "exit 7":
					if err == nil || errors.Is(err, ErrProbeTransient) || len(waits) != 0 {
						t.Fatalf("ordinary exit retried: %v %v", waits, err)
					}
				case recoverProbe:
					if err != nil || output != "capabilities\n" || !reflect.DeepEqual(waits, []time.Duration{time.Second}) {
						t.Fatalf("recovery: %q %v %v", output, err, waits)
					}
				default:
					if !errors.Is(err, ErrProbeTransient) || !reflect.DeepEqual(waits, []time.Duration{time.Second, 2 * time.Second}) {
						t.Fatalf("exhaustion: %v %v", err, waits)
					}
					if failure == "exec /bin/sleep 30" && !errors.Is(err, context.DeadlineExceeded) {
						t.Fatalf("timeout lost: %v", err)
					}
				}
			})
		}
	}
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func TestProbeCancellationAndMissingBinary(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	waits := 0
	_, err := conversationProbe(ctx, filepath.Join(t.TempDir(), "missing"), nil, nil, "", time.Second, func(ctx context.Context, _ time.Duration) error {
		waits++
		cancel()
		return ctx.Err()
	})
	if waits != 1 || !errors.Is(err, context.Canceled) || !errors.Is(err, ErrProbeTransient) {
		t.Fatalf("cancellation: %d %v", waits, err)
	}
}
