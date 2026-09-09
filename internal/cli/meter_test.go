package cli

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
)

func meterTestHome(t *testing.T) (*logsHome, store.Repo, store.MeterAttempt) {
	t.Helper()
	f := newLogsHome(t)
	if _, err := config.WriteDefault(config.NewPaths(f.home)); err != nil {
		t.Fatal(err)
	}
	repo := f.repo("acme", "api")
	session := f.session(repo, 73)
	run := f.allocateRun(session, 1, time.Time{})
	a, err := f.db.StartMeterAttempt(t.Context(), run.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.PutMeterInvocation(t.Context(), store.MeterInvocation{AttemptID: a.ID, ID: "root", SourceID: "source", SourceInvocationID: "root", Status: "partial"}); err != nil {
		t.Fatal(err)
	}
	return f, repo, a
}
func TestMeterExportFiltersAndCoverage(t *testing.T) {
	f, _, _ := meterTestHome(t)
	for _, args := range [][]string{
		{"meter", "export", "github:acme/api"},
		{"meter", "export", "github:acme/api", "--coding_agent", "codex", "--issue", "73", "--mode", "spike"},
		{"meter", "export", "github:acme/api", "--time_from", "2000-01-01T00:00:00Z", "--time_to", "2100-01-01T01:00:00+01:00"},
	} {
		code, out, diagnostics := f.run(args...)
		if code != 0 {
			t.Fatalf("%v: %d %s", args, code, diagnostics)
		}
		records, err := csv.NewReader(strings.NewReader(out)).ReadAll()
		if err != nil || len(records) != 2 {
			t.Fatalf("records %v %v", records, err)
		}
		if records[1][25] != "coverage" || records[1][12] != "" || records[1][20] != "" {
			t.Fatalf("fabricated coverage %v", records[1])
		}
	}
	for _, args := range [][]string{{"meter", "export", "github:acme/api", "--issue", "74"}, {"meter", "export", "github:acme/api", "--coding_agent", "claude"}, {"meter", "export", "github:acme/api", "--mode", "research"}, {"meter", "export", "github:acme/api", "--time_to", "2000-01-01T00:00:00Z"}} {
		code, out, errOut := f.run(args...)
		if code != 0 || out != meterCSVHeaderLine {
			t.Fatalf("empty %v: %d %q %s", args, code, out, errOut)
		}
	}
	for _, args := range [][]string{{"meter", "export"}, {"meter", "export", "acme/api"}, {"meter", "export", "github:missing/repo"}, {"meter", "export", "github:acme/api", "--issue", "0"}, {"meter", "export", "github:acme/api", "--issue", "-2"}, {"meter", "export", "github:acme/api", "--issue=-1"}, {"meter", "export", "github:acme/api", "--mode", "oops"}, {"meter", "export", "github:acme/api", "--coding_agent", "oops"}, {"meter", "export", "github:acme/api", "--time_from", "2026-01-01"}, {"meter", "export", "github:acme/api", "--time_from", "2026-01-01T00:00:00Z", "--time_to", "2026-01-01T00:00:00Z"}} {
		code, out, _ := f.run(args...)
		if code == 0 || out != "" {
			t.Fatalf("invalid %v: %d %q", args, code, out)
		}
	}
}
func TestMeterCSVTextRoundTrip(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"=SUM(A1)", " \t+42", "\rfoo", "\nfoo", "'original", "plain,\"quote\"\nnext", "@cmd", "", " model"} {
		model := text
		b, err := meterCSVBytes(store.MeterRow{ActualModel: &model})
		if err != nil {
			t.Fatal(err)
		}
		r, err := csv.NewReader(bytes.NewReader(b)).Read()
		if err != nil {
			t.Fatal(err)
		}
		got := r[10]
		got = strings.TrimPrefix(got, "'")
		if got != text {
			t.Fatalf("%q => %q", text, got)
		}
	}
}
func TestMeterSummaryUnknownModelRetained(t *testing.T) {
	f, _, _ := meterTestHome(t)
	code, out, errOut := f.run("meter", "acme/api", "73", "--model", "some-model", "--json", "--details")
	if code != 0 {
		t.Fatal(errOut)
	}
	var payload struct {
		Summary struct {
			Estimated  *string `json:"estimated_usd"`
			Collection string  `json:"collection_status"`
		} `json:"summary"`
		Details []store.MeterRow `json:"details"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Details) != 1 || payload.Summary.Estimated != nil || payload.Summary.Collection == "complete" {
		t.Fatalf("unknown hidden: %s", out)
	}
}

type meterBlockingWriter struct {
	first   bool
	entered chan struct{}
	release chan struct{}
}

func (w *meterBlockingWriter) Write(p []byte) (int, error) {
	if !w.first {
		w.first = true
		close(w.entered)
		<-w.release
	}
	return len(p), nil
}
func TestMeterExportReleasesDatabaseBeforeWriter(t *testing.T) {
	f, repo, a := meterTestHome(t)
	writer := &meterBlockingWriter{entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- writeMeter(t.Context(), writer, f.db, store.MeterFilter{RepoID: repo.ID}, meterOptions{}, true)
	}()
	select {
	case <-writer.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("waiting for CSV header")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, err := f.db.StartMeterAttempt(ctx, a.RunID, "claude")
	close(writer.release)
	if err != nil {
		t.Fatalf("writer retained SQLite: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("export did not finish")
	}
}

type meterBrokenWriter struct{}

func (meterBrokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestMeterExportBrokenWriterAndCancellation(t *testing.T) {
	f, repo, _ := meterTestHome(t)
	if err := writeMeter(t.Context(), meterBrokenWriter{}, f.db, store.MeterFilter{RepoID: repo.ID}, meterOptions{}, true); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var out bytes.Buffer
	if err := writeMeter(ctx, &out, f.db, store.MeterFilter{}, meterOptions{}, true); err == nil || out.Len() != 0 {
		t.Fatalf("cancel: %v %q", err, out.String())
	}
}

type meterShortWriter struct{}

func (meterShortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }
func TestMeterCSVShortHeader(t *testing.T) {
	f, _, _ := meterTestHome(t)
	if err := writeMeter(t.Context(), meterShortWriter{}, f.db, store.MeterFilter{}, meterOptions{}, true); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
}
func TestMeterExactTimeBoundsAndProjectIsolation(t *testing.T) {
	f, repo, _ := meterTestHome(t)
	other := f.repo("acme", "other")
	s := f.session(other, 73)
	r := f.allocateRun(s, 2, time.Time{})
	if _, err := f.db.StartMeterAttempt(t.Context(), r.ID, "codex"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.db.NewMeterSnapshot(t.Context(), store.MeterFilter{RepoID: repo.ID}, store.MeterSnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := snapshot.Next(t.Context(), 256)
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	start, err := time.Parse(time.RFC3339Nano, rows[0].AttemptStartedAt)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		from, to string
		want     int
	}{{start.Format(time.RFC3339Nano), "", 2}, {"", start.Format(time.RFC3339Nano), 1}, {start.Add(time.Nanosecond).Format(time.RFC3339Nano), "", 1}, {"", start.Add(time.Nanosecond).Format(time.RFC3339Nano), 2}, {start.In(time.FixedZone("offset", 3600)).Format(time.RFC3339Nano), "", 2}} {
		args := []string{"meter", "export", "github:acme/api", "--issue", "73"}
		if tc.from != "" {
			args = append(args, "--time_from", tc.from)
		}
		if tc.to != "" {
			args = append(args, "--time_to", tc.to)
		}
		code, out, errOut := f.run(args...)
		if code != 0 {
			t.Fatal(errOut)
		}
		records, err := csv.NewReader(strings.NewReader(out)).ReadAll()
		if err != nil || len(records) != tc.want {
			t.Fatalf("%v got %v %v", tc, records, err)
		}
	}
}

func TestMeterTextDetailsExposeMeasurementsAndProvenance(t *testing.T) {
	f, _, _ := meterTestHome(t)
	code, out, errOut := f.run("meter", "acme/api", "73", "--details")
	if code != 0 {
		t.Fatal(errOut)
	}
	for _, want := range []string{"invocation root", "price revision 1", "observed priced subtotal USD", "input_uncached", "cache_read", "cache_write_5m_subset", "cache_write_1h_subset", "reasoning_subset", "diagnostics"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %s", want, out)
		}
	}
}
