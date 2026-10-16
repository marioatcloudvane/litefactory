package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
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

func graphBugSyncExhaustionDiagnostic(ctx context.Context, db *store.DB, instanceID int64, node, diagnostic string) (string, error) {
	attempts, err := db.ListGraphNodeAttempts(ctx, instanceID)
	if err != nil {
		return "", err
	}
	var latest store.GraphNodeAttempt
	for _, attempt := range attempts {
		if attempt.Node == node && !attempt.FinishedAt.IsZero() && attempt.ID > latest.ID {
			latest = attempt
		}
	}
	if latest.ID == 0 {
		return diagnostic, nil
	}
	outputs, err := db.ListGraphOutputs(ctx, latest.ID)
	if err != nil {
		return "", err
	}
	var evidence ActivityResult
	for _, output := range outputs {
		switch output.Kind {
		case store.GraphOutputJSON:
			evidence.Output = output.Payload
		case store.GraphOutputDiagnostic:
			evidence.Diagnostic = string(output.Payload)
		}
	}
	reason := graphBugSyncDiagnostic(evidence)
	diagnostic += fmt.Sprintf("\nLast completed synchronization attempt: %d.", latest.ID)
	if reason != "" {
		diagnostic += "\n" + reason
	}
	return diagnostic, nil
}
