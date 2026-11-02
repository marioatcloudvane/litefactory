package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidateGoCacheEnvironment(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, content, helper, goenv string
		wantErr                      bool
	}{
		{name: "missing"},
		{name: "relative file", goenv: "relative-go.env", wantErr: true},
		{name: "module settings retained", content: "GOPROXY=https://example.invalid\nGONOSUMDB=example.invalid/private\n"},
		{name: "persisted external helper", content: "GOCACHEPROG=custom-helper\n", wantErr: true},
		{name: "empty process helper does not override persisted", content: "GOCACHEPROG=custom-helper\n", helper: "", wantErr: true},
		{name: "explicit external helper", helper: "custom-helper", wantErr: true},
		{name: "disabled file", content: "GOCACHEPROG=custom-helper\n", goenv: "off"},
		{name: "disabled file does not clear process helper", helper: "custom-helper", goenv: "off", wantErr: true},
		{name: "later empty setting wins", content: "GOCACHEPROG=custom-helper\nGOCACHEPROG=\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			file := filepath.Join(home, "go.env")
			if tt.content != "" {
				if err := os.WriteFile(file, []byte(tt.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			goenv := file
			if tt.goenv != "" {
				goenv = tt.goenv
			}
			before, err := os.ReadFile(file)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			err = ValidateGoCacheEnvironment([]string{"HOME=" + home, "GOENV=" + goenv, "GOCACHEPROG=" + tt.helper})
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%t", err, tt.wantErr)
			}
			after, err := os.ReadFile(file)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("Go configuration modified")
			}
		})
	}
}

func TestValidateGoCacheEnvironmentDefaultAndUnreadable(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	directory := filepath.Join(home, ".config")
	if runtime.GOOS == "darwin" {
		directory = filepath.Join(home, "Library", "Application Support")
	}
	if runtime.GOOS == "windows" {
		directory = filepath.Join(home, "appdata")
	}
	file := filepath.Join(directory, "go", "env")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("GOCACHEPROG=persisted-helper\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	environ := []string{"HOME=" + home, "APPDATA=" + directory}
	if err := ValidateGoCacheEnvironment(environ); err == nil || !strings.Contains(err.Error(), "GOCACHEPROG") {
		t.Fatalf("default env file bypassed: %v", err)
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		if err := ValidateGoCacheEnvironment([]string{"HOME=" + home, "XDG_CONFIG_HOME=" + directory}); err == nil {
			t.Fatal("XDG env file bypassed")
		}
	}
	if err := ValidateGoCacheEnvironment([]string{"GOENV=" + filepath.Dir(file)}); err == nil {
		t.Fatal("unreadable Go env accepted")
	}
}

func TestGoBuildCachesRejectPersistedHostCache(t *testing.T) {
	p, base := cacheFixture(t)
	hostCache := filepath.Join(base, "operator-host-cache")
	goenv := filepath.Join(base, "go.env")
	contents := "GOCACHE=" + hostCache + "\nGOPROXY=https://example.invalid\n"
	if err := os.WriteFile(goenv, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOENV", goenv)
	if _, err := p.ResolveGoBuildCaches(Settings{GoBuildCachePath: hostCache}); err == nil || !strings.Contains(err.Error(), "host GOCACHE") {
		t.Fatalf("persisted host cache accepted: %v", err)
	}
	if _, err := p.ResolveGoBuildCaches(Settings{GoBuildCachePath: filepath.Join(hostCache, "nested")}); err == nil {
		t.Fatal("nested persisted host cache accepted")
	}
	separate := filepath.Join(base, "dedicated-cache")
	caches, err := p.ResolveGoBuildCaches(Settings{GoBuildCachePath: separate})
	if err != nil {
		t.Fatal(err)
	}
	if caches.Main != separate {
		t.Fatalf("separate selection changed: %s", caches.Main)
	}
	after, err := os.ReadFile(goenv)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != contents {
		t.Fatal("persisted Go environment changed")
	}
	// A nonempty process value overrides the persisted host-cache location.
	t.Setenv("GOCACHE", filepath.Join(base, "effective-process-cache"))
	if _, err := p.ResolveGoBuildCaches(Settings{GoBuildCachePath: hostCache}); err != nil {
		t.Fatalf("overridden persisted host cache still rejected: %v", err)
	}
}
