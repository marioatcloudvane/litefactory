package config

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestHousekeepingSettings_Defaults(t *testing.T) {
	t.Parallel()
	d := Defaults()
	if d.Settings.DeployBackupKeep != 5 || DefaultDeployBackupKeep != 5 {
		t.Errorf("deploy_backup_keep default = %d, want 5", d.Settings.DeployBackupKeep)
	}
	if d.Settings.LogRetainDays != 30 || DefaultLogRetainDays != 30 {
		t.Errorf("log_retain_days default = %d, want 30", d.Settings.LogRetainDays)
	}
	if MinDeployBackupKeep != 1 || MaxDeployBackupKeep != 100 || MinLogRetainDays != 1 || MaxLogRetainDays != 3650 {
		t.Errorf("range constants wrong")
	}
}

func TestHousekeepingSettings_DecodeOmittedKeepsDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := Decode(bytes.NewReader(DefaultFileContents()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if cfg.Settings.DeployBackupKeep != 5 || cfg.Settings.LogRetainDays != 30 {
		t.Errorf("got %d/%d", cfg.Settings.DeployBackupKeep, cfg.Settings.LogRetainDays)
	}
}

func TestConfig_Validate_HousekeepingSettings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		keep    int
		days    int
		wantErr string
	}{
		{"both at lower bound/valid", 1, 1, ""},
		{"both at upper bound/valid", 100, 3650, ""},
		{"keep zero/rejected", 0, 30, "settings.deploy_backup_keep"},
		{"keep negative/rejected", -1, 30, "settings.deploy_backup_keep"},
		{"keep above max/rejected", 101, 30, "settings.deploy_backup_keep"},
		{"days zero/rejected", 5, 0, "settings.log_retain_days"},
		{"days negative/rejected", 5, -3, "settings.log_retain_days"},
		{"days above max/rejected", 5, 3651, "settings.log_retain_days"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := Decode(bytes.NewReader(DefaultFileContents()))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			cfg.Settings.DeployBackupKeep = tt.keep
			cfg.Settings.LogRetainDays = tt.days

			err = cfg.Validate()

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultFile_HousekeepingTemplateLines(t *testing.T) {
	t.Parallel()
	got := string(DefaultFileContents())
	want := []string{
		fmt.Sprintf("  deploy_backup_keep: 5 # newest pre-deploy database backups kept by daily cleanup; valid range: %d..%d", MinDeployBackupKeep, MaxDeployBackupKeep),
		fmt.Sprintf("  log_retain_days: 30 # days rotated global logs are kept before daily deletion; valid range: %d..%d", MinLogRetainDays, MaxLogRetainDays),
	}
	for _, line := range want {
		if !strings.Contains(got, line+"\n") {
			t.Errorf("template missing line %q", line)
		}
	}
}

func TestDecode_HousekeepingOverrides(t *testing.T) {
	t.Parallel()
	src := strings.Replace(string(DefaultFileContents()), "  deploy_backup_keep: 5", "  deploy_backup_keep: 12", 1)
	src = strings.Replace(src, "  log_retain_days: 30", "  log_retain_days: 90", 1)
	cfg, err := Decode(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if cfg.Settings.DeployBackupKeep != 12 || cfg.Settings.LogRetainDays != 90 {
		t.Errorf("got %d/%d, want 12/90", cfg.Settings.DeployBackupKeep, cfg.Settings.LogRetainDays)
	}
}
