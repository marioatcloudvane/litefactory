package bugchecks

import (
	"regexp"
	"strings"
)

// envRedactionMarker replaces a declared stack-profile environment value.
const envRedactionMarker = "[REDACTED]"

const (
	// minDeclaredValueLen is the length from which any declared value is
	// replaced literally, matching internal/redact's environment threshold.
	minDeclaredValueLen = 8
	// minSecretNamedValueLen is the length from which a value under a
	// secret-shaped variable name is replaced literally.
	minSecretNamedValueLen = 4
)

var secretEnvName = regexp.MustCompile(`(?i)TOKEN|SECRET|PASSWORD|PASSWD|KEY|CREDENTIAL`)

// redactDeclaredEnv replaces declared environment values in raw. Short values
// such as CI=1 are left alone so ordinary numbers in output stay readable;
// internal/redact still scans the result for secret shapes afterwards.
func redactDeclaredEnv(raw string, names []string, environment map[string]string) string {
	for _, name := range names {
		if value := environment[name]; shouldRedactDeclaredValue(name, value) {
			raw = strings.ReplaceAll(raw, value, envRedactionMarker)
		}
	}
	return raw
}

func shouldRedactDeclaredValue(name, value string) bool {
	secretName := secretEnvName.MatchString(name)
	if !secretName && isDigits(value) {
		return false
	}
	if len(value) >= minDeclaredValueLen {
		return true
	}
	return secretName && len(value) >= minSecretNamedValueLen
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
