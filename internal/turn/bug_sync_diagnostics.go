package turn

import (
	"encoding/json"
	"fmt"
	"strings"
)

// graphBugSyncDiagnostic makes the durable source evidence readable at the Bug
// owner-stop boundary. The original JSON remains attached to the attempt.
func graphBugSyncDiagnostic(activity ActivityResult) string {
	var evidence syncBaseOutput
	if json.Unmarshal(activity.Output, &evidence) != nil {
		return activity.Diagnostic
	}
	parts := []string{}
	if activity.Diagnostic != "" {
		parts = append(parts, activity.Diagnostic)
	}
	if evidence.Reason != "" && !strings.Contains(activity.Diagnostic, evidence.Reason) {
		parts = append(parts, evidence.Reason)
	}
	if evidence.Working != "" || evidence.Base != "" {
		parts = append(parts, fmt.Sprintf("Base synchronization: %s into %s.", evidence.Base, evidence.Working))
	}
	if len(evidence.Conflicts) > 0 {
		parts = append(parts, "Conflicting paths: "+strings.Join(evidence.Conflicts, ", ")+".")
	}
	if evidence.Publication != "" {
		parts = append(parts, "Publication: "+evidence.Publication+".")
	}
	if evidence.Commit != "" {
		parts = append(parts, "Synchronization commit: "+evidence.Commit+".")
	}
	if evidence.Commit != "" || evidence.Publication != "" {
		parts = append(parts, fmt.Sprintf("Retained checkout verified: %t.", evidence.CheckoutVerified))
	}
	return strings.Join(parts, "\n")
}
