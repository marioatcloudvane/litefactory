package bugchecks

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/redact"
)

// sanitizeLikeChecks mirrors the two production sites: declared values first,
// then the shared secret-shape redactor.
func sanitizeLikeChecks(t *testing.T, raw string, env map[string]string) string {
	t.Helper()
	names := make([]string, 0, len(env))
	for name := range env {
		names = append(names, name)
	}
	raw = redactDeclaredEnv(raw, names, env)
	body, err := redact.New(redact.Options{Environ: []string{}}).Apply(context.Background(), raw, OutputLimit)
	if err != nil {
		t.Fatal(err)
	}
	return body.String()
}

func TestDeclaredEnvRedactionKeepsOrdinaryNumbers(t *testing.T) {
	const summary = "3 failed, 269 passed in 427.3s"
	got := sanitizeLikeChecks(t, "CI=1 WORKERS=2\n"+summary, map[string]string{"CI": "1", "WORKERS": "2", "PORT": "8080", "BUILD_NUMBER": "123456789"})
	if !strings.Contains(got, summary) || !strings.Contains(got, "CI=1 WORKERS=2") || strings.Contains(got, envRedactionMarker) {
		t.Fatalf("ordinary numbers masked: %q", got)
	}
}

func TestDeclaredEnvRedactionStillHidesSecrets(t *testing.T) {
	pem := "-----BEGIN RSA PRIVATE KEY-----\nMIIB\n-----END RSA PRIVATE KEY-----"
	env := map[string]string{
		"GH_AUTH":       "ghp_AbCdEf0123456789AbCdEf0123456789",
		"AWS_ID":        "AKIAIOSFODNN7EXAMPLE",
		"API_TOKEN":     "s3cr3t-api-token-value-0123",
		"SIGNING_PEM":   pem,
		"DB_PASSWORD":   "hunt",
		"SHORT_API_KEY": "4567",
	}
	var raw strings.Builder
	for _, value := range env {
		raw.WriteString("value: " + value + "\n")
	}
	got := sanitizeLikeChecks(t, raw.String(), env)
	for name, value := range env {
		if strings.Contains(got, value) {
			t.Fatalf("%s leaked: %q", name, got)
		}
	}
	if !strings.Contains(got, envRedactionMarker) {
		t.Fatalf("declared marker missing: %q", got)
	}
}

func TestShouldRedactDeclaredValue(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		want        bool
	}{
		{"CI", "1", false},
		{"WORKERS", "2", false},
		{"BUILD_NUMBER", "123456789", false},
		{"MODE", "release", false},
		{"REGION", "eu-central-1", true},
		{"DB_PASSWORD", "hunt", true},
		{"SHORT_API_KEY", "4567", true},
		{"API_TOKEN", "abc", false},
		{"EMPTY", "", false},
	} {
		if got := shouldRedactDeclaredValue(tc.name, tc.value); got != tc.want {
			t.Errorf("shouldRedactDeclaredValue(%q, %q) = %v, want %v", tc.name, tc.value, got, tc.want)
		}
	}
}
