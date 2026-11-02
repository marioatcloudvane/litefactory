package process

import (
	"strings"
	"testing"
)

func TestDoctorEnvironmentKeepsUserButNoSecrets(t *testing.T) {
	env := DoctorEnvironment([]string{"PATH=/bin", "USER=owner", "LOGNAME=owner", "GITHUB_TOKEN=secret", "HOME=/Users/owner"}, "/scratch")
	joined := strings.Join(env, "\n")
	for _, want := range []string{"USER=owner", "LOGNAME=owner", "PATH=/bin", "HOME=/scratch"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in %v", want, env)
		}
	}
	if strings.Contains(joined, "GITHUB_TOKEN") || strings.Contains(joined, "HOME=/Users/owner") {
		t.Fatalf("ambient value kept: %v", env)
	}
}
