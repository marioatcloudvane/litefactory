package commandtext

import "strings"

// DiscussionSummary recognizes an explicit factory publication origin. Quoted
// markers remain ordinary owner text, as with the existing self filter.
func DiscussionSummary(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "<!-- lf:discussion-summary v=1 key=") {
			return true
		}
	}
	return false
}
