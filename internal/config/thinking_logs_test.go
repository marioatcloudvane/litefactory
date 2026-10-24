package config

import (
	"bytes"
	"strings"
	"testing"
)

func TestThinkingLogs_DefaultAndTemplate(t *testing.T) {
	t.Parallel()
	if got := Defaults().Settings.ThinkingLogs; got != ThinkingLogsNone {
		t.Fatalf("thinking_logs default = %q, want none", got)
	}
	contents := string(DefaultFileContents())
	line := "  thinking_logs: \"none\"\n"
	if !strings.Contains(contents, line) {
		t.Fatalf("config template does not carry thinking_logs:\n%s", contents)
	}
	cfg, err := Decode(strings.NewReader(strings.Replace(contents, line, "", 1)))
	if err != nil {
		t.Fatalf("Decode without key: %v", err)
	}
	if cfg.Settings.ThinkingLogs != ThinkingLogsNone || cfg.Settings.ThinkingLogs.Enabled() {
		t.Fatalf("omitted thinking_logs = %q, want none", cfg.Settings.ThinkingLogs)
	}
}

func TestThinkingLogs_Validate(t *testing.T) {
	t.Parallel()
	line := "  thinking_logs: \"none\"\n"
	for _, tt := range []struct {
		value   string
		want    ThinkingLogs
		wantErr bool
	}{
		{"none", ThinkingLogsNone, false},
		{"summarized", ThinkingLogsSummarized, false},
		{" Full ", ThinkingLogsFull, false},
		{"", "", true},
		{"detailed", "", true},
		{"raw", "", true},
	} {
		contents := strings.Replace(string(DefaultFileContents()), line, "  thinking_logs: \""+tt.value+"\"\n", 1)
		cfg, err := Decode(bytes.NewReader([]byte(contents)))
		if err == nil {
			err = cfg.Validate()
		}
		if tt.wantErr {
			if err == nil || !strings.Contains(err.Error(), "settings.thinking_logs") {
				t.Errorf("thinking_logs %q: err = %v, want a settings.thinking_logs error", tt.value, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("thinking_logs %q: unexpected error %v", tt.value, err)
			continue
		}
		if cfg.Settings.ThinkingLogs != tt.want {
			t.Errorf("thinking_logs %q decoded as %q, want %q", tt.value, cfg.Settings.ThinkingLogs, tt.want)
		}
	}
}
