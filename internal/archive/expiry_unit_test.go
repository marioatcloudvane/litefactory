package archive_test

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/archive"
)

func gzBytes(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func headerLine(t *testing.T, kind, at string) string {
	t.Helper()
	b, err := json.Marshal(archive.Header{Kind: kind, Format: archive.FormatName, Version: 1, ArchivedAt: at, TaskRef: "github:acme/api#1", Repo: "acme/api", Issue: 1})
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

func TestReadHeaderFrom(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
		wantAt  string
	}{
		{"valid header then junk/returns header only", gzBytes(t, headerLine(t, archive.KindHeader, "2026-01-02T03:04:05Z")+"not json\n"), false, "2026-01-02T03:04:05Z"},
		{"not gzip/returns error", []byte("plain text"), true, ""},
		{"empty gzip/returns error", gzBytes(t, ""), true, ""},
		{"invalid json/returns error", gzBytes(t, "{nope\n"), true, ""},
		{"first record not header/returns error", gzBytes(t, headerLine(t, "row", "2026-01-02T03:04:05Z")), true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, err := archive.ReadHeaderFrom(bytes.NewReader(tt.data))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got header %+v", h)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if h.ArchivedAt != tt.wantAt || h.Kind != archive.KindHeader {
				t.Errorf("header = %+v", h)
			}
		})
	}
}

func TestReadHeader(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "a.jsonl.gz")
	if err := os.WriteFile(p, gzBytes(t, headerLine(t, archive.KindHeader, "2026-05-06T07:08:09Z")), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := archive.ReadHeader(p)
	if err != nil || h.ArchivedAt != "2026-05-06T07:08:09Z" {
		t.Fatalf("got %+v, %v", h, err)
	}
	if _, err := archive.ReadHeader(filepath.Join(dir, "missing.jsonl.gz")); err == nil {
		t.Error("missing file: want error")
	}
}

func TestHeaderDate(t *testing.T) {
	t.Parallel()
	got, err := archive.HeaderDate(archive.Header{ArchivedAt: "2026-01-02T05:04:05+02:00"})
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if !got.Equal(want) || got.Location() != time.UTC {
		t.Errorf("got %v, want %v UTC", got, want)
	}
	for _, bad := range []string{"", "yesterday", "2026-01-02"} {
		if _, err := archive.HeaderDate(archive.Header{ArchivedAt: bad}); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
}

func TestChooseDate(t *testing.T) {
	t.Parallel()
	a := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b := a.Add(time.Hour)
	m := a.Add(-48 * time.Hour)
	tests := []struct {
		name        string
		nameT       time.Time
		nameOK      bool
		hdr         time.Time
		hdrOK       bool
		wantDate    time.Time
		wantSource  string
		wantWarning bool
	}{
		{"equal dates/uses name without warning", a, true, a, true, a, archive.DateSourceName, false},
		{"header later/uses header with warning", a, true, b, true, b, archive.DateSourceHeader, true},
		{"name later/uses name with warning", b, true, a, true, b, archive.DateSourceName, true},
		{"name only/uses name", a, true, time.Time{}, false, a, archive.DateSourceName, false},
		{"header only/uses header", time.Time{}, false, b, true, b, archive.DateSourceHeader, false},
		{"neither/uses mtime", time.Time{}, false, time.Time{}, false, m, archive.DateSourceMtime, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := archive.ChooseDate(tt.nameT, tt.nameOK, tt.hdr, tt.hdrOK, m)
			if !got.Date.Equal(tt.wantDate) || got.Source != tt.wantSource {
				t.Errorf("got %v/%s, want %v/%s", got.Date, got.Source, tt.wantDate, tt.wantSource)
			}
			if (got.Warning != "") != tt.wantWarning {
				t.Errorf("warning = %q, want warning=%v", got.Warning, tt.wantWarning)
			}
			if tt.wantWarning && !strings.Contains(got.Warning, "differ") {
				t.Errorf("warning %q does not mention difference", got.Warning)
			}
		})
	}
}

func TestExpired(t *testing.T) {
	t.Parallel()
	date := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	edge := date.Add(30 * 24 * time.Hour)
	tests := []struct {
		name string
		now  time.Time
		date time.Time
		days int
		want bool
	}{
		{"exactly at date plus days/expired", edge, date, 30, true},
		{"one nanosecond before/not expired", edge.Add(-time.Nanosecond), date, 30, false},
		{"one nanosecond after/expired", edge.Add(time.Nanosecond), date, 30, true},
		{"future date/not expired", date, date.Add(time.Hour), 1, false},
		{"zero days/never expired", edge.Add(time.Hour * 1000), date, 0, false},
		{"negative days/never expired", edge.Add(time.Hour * 1000), date, -5, false},
		{"one day exactly/expired", date.Add(24 * time.Hour), date, 1, true},
		{"now equals date with one day/not expired", date, date, 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := archive.Expired(tt.now, tt.date, tt.days); got != tt.want {
				t.Errorf("Expired = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseName_ExpiryInputs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		in        string
		wantIssue int
		wantErr   bool
	}{
		{"canonical/parses", "413-20260102T030405Z.jsonl.gz", 413, false},
		{"partial/rejected", "413-20260102T030405Z.jsonl.gz.partial", 0, true},
		{"bad timestamp/rejected", "413-20261302T030405Z.jsonl.gz", 0, true},
		{"missing issue/rejected", "-20260102T030405Z.jsonl.gz", 0, true},
		{"wrong extension/rejected", "413-20260102T030405Z.jsonl", 0, true},
		{"path prefix/rejected", "x/413-20260102T030405Z.jsonl.gz", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			issue, ts, err := archive.ParseName(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error")
				}
				return
			}
			if err != nil || issue != tt.wantIssue || !ts.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
				t.Errorf("got %d %v %v", issue, ts, err)
			}
		})
	}
}
