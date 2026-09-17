package report

import (
	"regexp"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
)

// storyIDPattern confines the ID interpolated into story markers.
var storyIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$`)

// Story is the complete publication input for RenderCompleteStory.
// The factory validates the full proposal before invoking the renderer.
type Story struct {
	Ref   naming.Ref
	ID    string
	Title string
	// Excerpt holds the complete specification, which must never be shortened.
	Excerpt string
}

// SubIssue contains redacted publication bytes and their stable story marker.
type SubIssue struct {
	Title  redact.Body
	Body   redact.Body
	Marker string
}
