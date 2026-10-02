package config

import (
	"fmt"
	"strings"
	"testing"
)

func TestArchiveRetainDays_DefaultAndBounds(t *testing.T) {
	t.Parallel()
	if DefaultArchiveRetainDays != 30 || Defaults().Settings.ArchiveRetainDays != 30 {
		t.Errorf("default = %d, want 30", Defaults().Settings.ArchiveRetainDays)
	}
	if MinArchiveRetainDays != 1 || MaxArchiveRetainDays != 3650 {
		t.Errorf("range = %d..%d, want 1..3650", MinArchiveRetainDays, MaxArchiveRetainDays)
	}
}

func TestConfig_Validate_ArchiveRetainDays(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		days    int
		wantErr bool
	}{
		{"lower bound/valid", 1, false},
		{"default/valid", 30, false},
		{"upper bound/valid", 3650, false},
		{"zero/rejected", 0, true},
		{"negative/rejected", -1, true},
		{"above max/rejected", 3651, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, derr := Decode(strings.NewReader(string(DefaultFileContents())))
			if derr != nil {
				t.Fatal(derr)
			}
			cfg.Settings.ArchiveRetainDays = tt.days
			err := cfg.Validate()
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "settings.archive_retain_days") {
				t.Fatalf("error = %v, want mention of settings.archive_retain_days", err)
			}
		})
	}
}

func TestDefaultFile_ArchiveRetainDaysLine(t *testing.T) {
	t.Parallel()
	want := fmt.Sprintf("  archive_retain_days: 30 # days archive files and the run logs they cover are kept before daily deletion; valid range: %d..%d\n", MinArchiveRetainDays, MaxArchiveRetainDays)
	if !strings.Contains(string(DefaultFileContents()), want) {
		t.Errorf("template missing line %q", want)
	}
}

func TestDecode_ArchiveRetainDaysOverrideAndOmitted(t *testing.T) {
	t.Parallel()
	def := string(DefaultFileContents())
	cfg, err := Decode(strings.NewReader(strings.Replace(def, "  archive_retain_days: 30", "  archive_retain_days: 7", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Settings.ArchiveRetainDays != 7 {
		t.Errorf("override = %d, want 7", cfg.Settings.ArchiveRetainDays)
	}
	var kept []string
	for _, l := range strings.Split(def, "\n") {
		if !strings.Contains(l, "archive_retain_days") {
			kept = append(kept, l)
		}
	}
	cfg, err = Decode(strings.NewReader(strings.Join(kept, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Settings.ArchiveRetainDays != 30 {
		t.Errorf("omitted = %d, want default 30", cfg.Settings.ArchiveRetainDays)
	}
}
