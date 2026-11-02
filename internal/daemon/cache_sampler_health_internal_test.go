package daemon

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/diskstatus"
	"github.com/cloudvane/litefactory/internal/store"
)

// This is called for both observations from the same over-limit fixture so the
// actual partial and complete measurements traverse the production projection,
// SQL serialization, readback and shared status formatter.
func assertCacheSampleHealth(t *testing.T, s *cacheSampler, main, state string, bytes int64) {
	t.Helper()
	d := &Daemon{cacheSampler: s, diskPath: main, minFreeDiskBytes: 100, freeDiskBytes: func(string) (uint64, error) { return 101, nil }, log: slog.New(slog.DiscardHandler)}
	now := time.Now()
	if d.observeDisk(t.Context(), now) {
		t.Fatal("size observation changed admission")
	}
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "health.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	observation := d.diskStatus()
	if err := db.ReplaceHealthSnapshot(t.Context(), store.ListenerStatus{Disk: observation}, nil); err != nil {
		t.Fatal(err)
	}
	snap, err := db.ReadHealthSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Listener.Disk == nil {
		t.Fatal("persisted observation absent")
	}
	got := snap.Listener.Disk.Main
	if got.Path != main || got.State != state || got.PartialBytes != bytes || !got.PassStartedAt.Equal(observation.Main.PassStartedAt) || !got.ProgressAt.Equal(observation.Main.ProgressAt) {
		t.Fatalf("status lost sampler evidence: %+v", got)
	}
	if state == "partial" && got.CompleteBytes != nil {
		t.Fatal("partial became complete")
	}
	if state == "complete" && (got.CompleteBytes == nil || *got.CompleteBytes != bytes) {
		t.Fatal("complete total lost")
	}
	rendered := diskstatus.Text(snap.Listener.Disk, now)
	if !strings.Contains(rendered, "size_state="+state) || !strings.Contains(rendered, fmt.Sprintf("observed partial logical bytes=%d", bytes)) || !strings.Contains(rendered, "automatic_cleanup=unavailable") {
		t.Fatalf("status lost labels: %s", rendered)
	}
}
