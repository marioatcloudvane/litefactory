package daemon

import (
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestDiskGuardStatusBoundaries(t *testing.T) {
	t.Parallel()
	const stop uint64 = 2 << 30
	for _, tt := range []struct {
		name string
		free uint64
		held bool
	}{{"below", stop - 1, true}, {"equal", stop, false}, {"above", stop + 1, false}} {
		t.Run(tt.name, func(t *testing.T) {
			d := &Daemon{minFreeDiskBytes: stop, diskPath: "/fixture/factory", log: slog.New(slog.NewTextHandler(io.Discard, nil)), freeDiskBytes: func(string) (uint64, error) { return tt.free, nil }}
			if held := d.lowDisk(t.Context()); held != tt.held {
				t.Fatalf("held=%t want %t", held, tt.held)
			}
			o := d.diskStatus()
			if o == nil || o.Held != tt.held || !o.GuardEnabled || o.StopBytes != stop || o.FreeBytes == nil || *o.FreeBytes != tt.free {
				t.Fatalf("incorrect observation: %+v", o)
			}
		})
	}
}

func TestDiskGuardStatusProbeErrorsAndDisabledDisplay(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		for _, failed := range []bool{false, true} {
			name := "disabled"
			if enabled {
				name = "enabled"
			}
			if failed {
				name += " failed"
			}
			t.Run(name, func(t *testing.T) {
				calls := 0
				d := &Daemon{diskPath: "/fixture/factory", log: slog.New(slog.NewTextHandler(io.Discard, nil)), freeDiskBytes: func(string) (uint64, error) {
					calls++
					if failed {
						return 0, errors.New("fixture probe unavailable")
					}
					return 1, nil
				}}
				if enabled {
					d.minFreeDiskBytes = 2 << 30
				}
				if enabled {
					if held := d.lowDisk(t.Context()); held == failed {
						t.Fatalf("enabled guard held=%t on failed=%t", held, failed)
					}
				} else {
					if d.lowDisk(t.Context()) || calls != 0 {
						t.Fatal("disabled guard probed or held admission")
					}
					if d.observeDisk(t.Context(), time.Now()) {
						t.Fatal("display-only observation held admission")
					}
				}
				o := d.diskStatus()
				if calls != 1 || o == nil || o.GuardEnabled != enabled || o.Held != (enabled && !failed) {
					t.Fatalf("incorrect probe state: calls=%d observation=%+v", calls, o)
				}
				if failed && (o.FreeBytes != nil || o.ProbeState != "unknown" || o.ProbeError == "") {
					t.Fatalf("failed probe became fabricated measurement: %+v", o)
				}
			})
		}
	}
}

func TestDiskGuardStatusSkippedAdmissionClearsOnlyHold(t *testing.T) {
	t.Parallel()
	d := &Daemon{minFreeDiskBytes: 2 << 30, diskPath: "/fixture/factory", log: slog.New(slog.NewTextHandler(io.Discard, nil)), freeDiskBytes: func(string) (uint64, error) { return 1, nil }}
	if !d.lowDisk(t.Context()) {
		t.Fatal("fixture did not hold admission")
	}
	before := d.diskStatus()
	d.clearDiskHold()
	after := d.diskStatus()
	if after.Held || after.FreeBytes == nil || *after.FreeBytes != 1 || !after.ObservedAt.Equal(before.ObservedAt) || !after.GuardEnabled {
		t.Fatalf("clearing actual hold changed measurement: before=%+v after=%+v", before, after)
	}
}
