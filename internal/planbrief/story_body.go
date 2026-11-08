package planbrief

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// MaxProposalBytes bounds untrusted typed Plan content and the Story Index
// before allocation.
const MaxProposalBytes = 8 * 1024 * 1024

// MaxStoryBodyBytes bounds complete story content accepted for publication.
const MaxStoryBodyBytes = 65536

// proposalKey is the shape of a story key.
var proposalKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$`)

// StoryHash binds both editable fields; timestamps alone cannot detect edits
// within GitHub's timestamp precision.
func StoryHash(title, body string) string {
	sum := sha256.Sum256([]byte(title + "\x00" + body))
	return hex.EncodeToString(sum[:])
}

// ValidateStoryBody requires independently usable story sections. The complete
// Markdown survives publication; no excerpt extraction or truncation is allowed.
func ValidateStoryBody(body string) error {
	if len(body) > MaxStoryBodyBytes {
		return fmt.Errorf("body exceeds issue size limit")
	}
	sections := map[string]bool{}
	current := ""
	fenced := false
	for line := range strings.SplitSeq(body, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		if strings.HasPrefix(t, "## ") {
			current = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(t, "## ")))
			continue
		}
		if current != "" && t != "" {
			sections[current] = true
		}
	}
	var faults []error
	for _, name := range []string{"user story", "acceptance criteria", "tasks", "architecture", "dependencies"} {
		if !sections[name] {
			faults = append(faults, fmt.Errorf("missing nonempty %s section", name))
		}
	}
	if _, err := AcceptanceCriteria(body); err != nil {
		faults = append(faults, err)
	}
	return errors.Join(faults...)
}
