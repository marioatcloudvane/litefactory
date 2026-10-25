package config

import (
	"bytes"
	"strings"
	"testing"
)

func TestMinFreeDiskSettings_DefaultAndTemplate(t *testing.T) {
	t.Parallel()
	if d := Defaults(); d.Settings.MinFreeDiskGB != 2 || DefaultMinFreeDiskGB != 2 {
		t.Fatalf("min_free_disk_gb default = %d, want 2", d.Settings.MinFreeDiskGB)
	}
	contents := DefaultFileContents()
	if !strings.Contains(string(contents), "  min_free_disk_gb: 2 # valid range: 0..10000\n") {
		t.Fatalf("config template does not document min_free_disk_gb:\n%s", contents)
	}
	cfg, err := Decode(bytes.NewReader(contents))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if cfg.Settings.MinFreeDiskGB != 2 || cfg.Settings.MinFreeDiskBytes() != 2<<30 {
		t.Fatalf("decoded min_free_disk_gb = %d (%d bytes)", cfg.Settings.MinFreeDiskGB, cfg.Settings.MinFreeDiskBytes())
	}
	stripped := strings.Replace(string(contents), "  min_free_disk_gb: 2 # valid range: 0..10000\n", "", 1)
	cfg, err = Decode(strings.NewReader(stripped))
	if err != nil {
		t.Fatalf("Decode without key: %v", err)
	}
	if cfg.Settings.MinFreeDiskGB != 2 {
		t.Fatalf("omitted min_free_disk_gb = %d, want default 2", cfg.Settings.MinFreeDiskGB)
	}
	explicit := strings.Replace(string(contents), "  min_free_disk_gb: 2 # valid range: 0..10000\n", "  min_free_disk_gb: 15\n", 1)
	cfg, err = Decode(strings.NewReader(explicit))
	if err != nil {
		t.Fatalf("Decode explicit key: %v", err)
	}
	if cfg.Settings.MinFreeDiskGB != 15 || cfg.Settings.MinFreeDiskBytes() != 15<<30 {
		t.Fatalf("explicit min_free_disk_gb = %d, want override 15", cfg.Settings.MinFreeDiskGB)
	}
}

func TestMinFreeDiskSettings_Validate(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		gb      int
		wantErr bool
	}{{0, false}, {1, false}, {15, false}, {10000, false}, {-1, true}, {10001, true}} {
		cfg, err := Decode(bytes.NewReader(DefaultFileContents()))
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		cfg.Settings.MinFreeDiskGB = tt.gb
		err = cfg.Validate()
		if tt.wantErr != (err != nil && strings.Contains(err.Error(), "settings.min_free_disk_gb")) {
			t.Errorf("min_free_disk_gb %d: err = %v, wantErr %v", tt.gb, err, tt.wantErr)
		}
		if !tt.wantErr && err != nil {
			t.Errorf("min_free_disk_gb %d: unexpected error %v", tt.gb, err)
		}
	}
	if got := (Settings{MinFreeDiskGB: 0}).MinFreeDiskBytes(); got != 0 {
		t.Errorf("0 GB = %d bytes, want 0 (disabled)", got)
	}
}
