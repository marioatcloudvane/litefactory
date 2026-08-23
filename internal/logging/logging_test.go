package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestNewFansOutToBothSinks(t *testing.T) {
	t.Parallel()

	var console, file bytes.Buffer
	logger := New(Options{Console: &console, File: &file})
	logger.Info("hello", slog.String("issue", "123"))

	if got := console.String(); !strings.Contains(got, "msg=hello") || !strings.Contains(got, "issue=123") {
		t.Errorf("console text record = %q, want the message and its attribute", got)
	}

	var rec map[string]any
	if err := json.Unmarshal(file.Bytes(), &rec); err != nil {
		t.Fatalf("file record is not JSON: %v (%q)", err, file.String())
	}
	if rec["msg"] != "hello" || rec["issue"] != "123" {
		t.Errorf("file record = %v, want msg=hello issue=123", rec)
	}
}

func TestNewWithoutConsoleWritesOnlyJSON(t *testing.T) {
	t.Parallel()

	var file bytes.Buffer
	New(Options{File: &file}).Info("hello")
	if !strings.HasPrefix(file.String(), "{") {
		t.Errorf("record = %q, want JSON", file.String())
	}
}

func TestNewWithoutSinksDiscards(t *testing.T) {
	t.Parallel()

	logger := New(Options{})
	logger.Info("hello")
	logger.With(slog.String("k", "v")).WithGroup("g").Error("boom")
}

func TestNewRespectsLevel(t *testing.T) {
	t.Parallel()

	var console, file bytes.Buffer
	logger := New(Options{Level: slog.LevelWarn, Console: &console, File: &file})
	logger.Info("dropped")
	logger.Warn("kept")

	for name, buf := range map[string]*bytes.Buffer{"console": &console, "file": &file} {
		if strings.Contains(buf.String(), "dropped") {
			t.Errorf("%s emitted a record below the configured level: %q", name, buf)
		}
		if !strings.Contains(buf.String(), "kept") {
			t.Errorf("%s dropped a record at the configured level: %q", name, buf)
		}
	}
}

func TestFanoutPreservesAttrsAndGroups(t *testing.T) {
	t.Parallel()

	var console, file bytes.Buffer
	logger := New(Options{Console: &console, File: &file}).
		With(slog.String("run", "7")).
		WithGroup("git")
	logger.Info("push", slog.String("branch", "main"))

	if got := console.String(); !strings.Contains(got, "run=7") || !strings.Contains(got, "git.branch=main") {
		t.Errorf("console record = %q, want the attribute and the group", got)
	}
	if got := file.String(); !strings.Contains(got, `"run":"7"`) || !strings.Contains(got, `"git":{"branch":"main"}`) {
		t.Errorf("file record = %q, want the attribute and the group", got)
	}
}

func TestParseLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in      string
		want    slog.Level
		wantErr bool
	}{
		{in: "", want: slog.LevelInfo},
		{in: "debug", want: slog.LevelDebug},
		{in: "INFO", want: slog.LevelInfo},
		{in: "warn", want: slog.LevelWarn},
		{in: "error", want: slog.LevelError},
		{in: "loud", wantErr: true},
	}
	for _, tc := range tests {
		got, err := ParseLevel(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseLevel(%q) succeeded, want an error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseLevel(%q) returned %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestIsTerminal(t *testing.T) {
	t.Parallel()

	if IsTerminal(nil) {
		t.Error("IsTerminal(nil) = true, want false")
	}

	f, err := os.CreateTemp(t.TempDir(), "not-a-tty")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Errorf("close temp file: %v", err)
		}
	}()

	if IsTerminal(f) {
		t.Error("IsTerminal(regular file) = true, want false")
	}
}
