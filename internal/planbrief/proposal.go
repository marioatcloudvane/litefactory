package planbrief

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// ProposalVersion identifies the strict transient proposal wire format.
const ProposalVersion = 1

// ProposalFileName is the reserved worktree handoff consumed by the factory.
const ProposalFileName = ".litefactory-plan-proposal.json"

// MaxProposalBytes bounds untrusted proposal decoding before allocation.
const MaxProposalBytes = 8 * 1024 * 1024

// MaxStoryBodyBytes bounds complete story content accepted for publication.
const MaxStoryBodyBytes = 65536

// PlanProposal is ephemeral transport, never a second editable story source.
// Parent and generation must also match factory-supplied execution context.
type PlanProposal struct {
	Version    int             `json:"version"`
	Parent     string          `json:"parent"`
	Generation int             `json:"generation"`
	Stories    []StoryProposal `json:"stories"`
}

// StoryProposal binds a complete story edit to its identity and expected base revision.
type StoryProposal struct {
	Key          string   `json:"key"`
	Intent       string   `json:"intent"`
	IssueNumber  int      `json:"issue_number"`
	IssueID      int64    `json:"issue_id"`
	ExpectedBase string   `json:"expected_base"`
	Title        string   `json:"title"`
	Body         string   `json:"body"`
	Dependencies []string `json:"dependencies"`
}

var proposalKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$`)

// StoryHash binds both editable fields; timestamps alone cannot detect edits
// within GitHub's timestamp precision.
func StoryHash(title, body string) string {
	sum := sha256.Sum256([]byte(title + "\x00" + body))
	return hex.EncodeToString(sum[:])
}

// ParseProposal rejects oversized, unknown-field and trailing-data input before validation.
func ParseProposal(data []byte) (PlanProposal, error) {
	var p PlanProposal
	if len(data) > MaxProposalBytes {
		return p, fmt.Errorf("plan proposal exceeds size limit")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, fmt.Errorf("parse plan proposal: %w", err)
	}
	if err := d.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return p, fmt.Errorf("plan proposal has trailing data")
	}
	if err := p.Validate(); err != nil {
		return PlanProposal{}, err
	}
	return p, nil
}

// Validate checks the full proposal and dependency graph before any forge mutation.
func (p PlanProposal) Validate() error {
	var faults []error
	if p.Version != ProposalVersion || p.Parent == "" || p.Generation < 1 || len(p.Stories) == 0 || len(p.Stories) > 100 {
		faults = append(faults, fmt.Errorf("invalid plan proposal envelope"))
	}
	keys := map[string]StoryProposal{}
	numbers := map[int]bool{}
	ids := map[int64]bool{}
	for _, s := range p.Stories {
		if !proposalKey.MatchString(s.Key) {
			faults = append(faults, fmt.Errorf("invalid story key"))
		}
		if _, ok := keys[s.Key]; ok {
			faults = append(faults, fmt.Errorf("story %s duplicated", s.Key))
		}
		keys[s.Key] = s
		if err := ValidateStoryBody(s.Body); err != nil {
			faults = append(faults, fmt.Errorf("story %s: %w", s.Key, err))
		}
		if strings.TrimSpace(s.Title) == "" || len(s.Title) > 256 || strings.ContainsAny(s.Title, "\r\n") {
			faults = append(faults, fmt.Errorf("story %s: invalid title", s.Key))
		}
		switch s.Intent {
		case "create":
			if s.IssueNumber != 0 || s.IssueID != 0 || s.ExpectedBase != "" {
				faults = append(faults, fmt.Errorf("story %s: create carries existing identity", s.Key))
			}
		case "update", "unchanged":
			if s.IssueNumber < 1 || s.IssueID < 1 || len(s.ExpectedBase) != 64 {
				faults = append(faults, fmt.Errorf("story %s: missing base identity", s.Key))
			}
			if numbers[s.IssueNumber] || ids[s.IssueID] {
				faults = append(faults, fmt.Errorf("story %s: duplicate issue identity", s.Key))
			}
			numbers[s.IssueNumber] = true
			ids[s.IssueID] = true
		default:
			faults = append(faults, fmt.Errorf("story %s: invalid intent", s.Key))
		}
	}
	for _, s := range p.Stories {
		seen := map[string]bool{}
		for _, dep := range s.Dependencies {
			if _, ok := keys[dep]; !ok || dep == s.Key || seen[dep] {
				faults = append(faults, fmt.Errorf("story %s: invalid dependency %s", s.Key, dep))
			}
			seen[dep] = true
		}
	}
	visiting := map[string]bool{}
	done := map[string]bool{}
	var visit func(string) bool
	visit = func(k string) bool {
		if visiting[k] {
			return false
		}
		if done[k] {
			return true
		}
		visiting[k] = true
		for _, dep := range keys[k].Dependencies {
			if !visit(dep) {
				return false
			}
		}
		visiting[k] = false
		done[k] = true
		return true
	}
	for _, story := range p.Stories {
		k := story.Key
		if !visit(k) {
			faults = append(faults, fmt.Errorf("story %s: dependency cycle", k))
			break
		}
	}
	return errors.Join(faults...)
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
