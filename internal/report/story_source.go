package report

import (
	"fmt"
	"github.com/cloudvane/litefactory/internal/naming"
)

// Only validated identifiers create links; arbitrary API-provided URLs never
// cross the rendering boundary.
func storySource(ref naming.Ref, child int, comment int64) string {
	if child <= 0 || comment <= 0 || ref.Validate() != nil {
		return ""
	}
	return fmt.Sprintf("\n\nParent plan: https://github.com/%s/%s/issues/%d · source: https://github.com/%s/%s/issues/%d#issuecomment-%d", ref.Owner, ref.Repo, ref.Issue, ref.Owner, ref.Repo, child, comment)
}

// StoryAcknowledgementMarker identifies the delivery owed by a durable admission.
func StoryAcknowledgementMarker(ref naming.Ref, generation, child int, comment int64) string {
	if ref.Validate() != nil || generation < 1 || child < 1 || comment < 1 {
		return ""
	}
	return fmt.Sprintf("<!-- lf:story-ack %s generation=%d child=%d comment=%d -->", ref.String(), generation, child, comment)
}
