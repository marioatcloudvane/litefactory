package bugchecks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadObservedRefusesEscapesAndBounds(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "report")
	if err := os.WriteFile(file, []byte("structured"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := readObserved(root, file)
	if err != nil || got != "structured" {
		t.Fatalf("got %q err=%v", got, err)
	}
	outside := filepath.Join(t.TempDir(), "other")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readObserved(root, link); err == nil {
		t.Fatal("symlink escape accepted")
	}
	if err := os.WriteFile(file, []byte(strings.Repeat("x", OutputLimit+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readObserved(root, file); err == nil {
		t.Fatal("oversized report accepted")
	}
}

func TestObservedReporterPrecedesVSTestRunSettings(t *testing.T) {
	original := []string{"dotnet", "test", "Tests.csproj", "--", "RunConfiguration.X=true"}
	actual := runnerArgs(original, []string{"--logger", "trx"})
	if strings.Join(actual, " ") != "dotnet test Tests.csproj --logger trx -- RunConfiguration.X=true" {
		t.Fatalf("wrong reporter boundary: %q", actual)
	}
	if strings.Join(original, " ") != "dotnet test Tests.csproj -- RunConfiguration.X=true" {
		t.Fatal("original recipe mutated")
	}
}
