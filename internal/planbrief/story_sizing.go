package planbrief

import (
	"strings"
	"unicode/utf8"
)

// ValidateStorySizing validates saved facts independently of routing or fallback.
// Missing route metadata remains compatible with earlier saved plans.
func ValidateStorySizing(size, reason string, routeVersion *int) (bool, string) {
	if size != "S" && size != "M" && size != "L" {
		return false, "size must be exactly S, M or L"
	}
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return false, "size reason is blank"
	}
	if !utf8.ValidString(reason) {
		return false, "size reason is not valid UTF-8"
	}
	if strings.ContainsAny(reason, "\r\n") {
		return false, "size reason contains a line break"
	}
	if utf8.RuneCountInString(trimmed) > 280 {
		return false, "size reason exceeds 280 characters"
	}
	if routeVersion != nil && *routeVersion != PlanParallelVersion {
		return false, "unsupported Plan route version"
	}
	return true, ""
}
