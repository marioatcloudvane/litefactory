package submission

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/naming"
)

// CreationEnvelope contains the exact reviewed bytes and relationships. The
// future tool family renders/normalizes content before retaining this envelope.
// There is no grant, credential or caller-selected permission in this format.
type CreationEnvelope struct {
	Version      int                `json:"version"`
	Repository   string             `json:"repository"`
	SourceTask   string             `json:"source_task"`
	Relationship string             `json:"relationship"`
	Artifacts    []CreationArtifact `json:"artifacts"`
}

// CreationArtifact contains one exact publishable issue and its relationship targets.
type CreationArtifact struct {
	Title       string `json:"title"`
	Body        string `json:"body"`
	ParentIssue int    `json:"parent_issue,omitempty"`
	LinkIssues  []int  `json:"link_issues,omitempty"`
}

// CanonicalCreation validates the bounded versioned envelope and hashes the
// deterministic typed encoding, never a supplied hash or a set of IDs alone.
func CanonicalCreation(e CreationEnvelope) ([]byte, string, error) {
	ref, err := naming.ParseRef(e.SourceTask)
	if err != nil || ref.String() != e.SourceTask || ref.Owner+"/"+ref.Repo != e.Repository || e.Version != 1 || (e.Relationship != "linked" && e.Relationship != "attach") || len(e.Artifacts) < 1 || len(e.Artifacts) > 5 {
		return nil, "", errors.New("invalid exact creation envelope")
	}
	for _, a := range e.Artifacts {
		if strings.TrimSpace(a.Title) == "" || len(a.Title) > 256 || strings.TrimSpace(a.Body) == "" || len(a.Body) > 65536 || !utf8.ValidString(a.Title) || !utf8.ValidString(a.Body) || a.ParentIssue < 0 || len(a.LinkIssues) > 16 {
			return nil, "", errors.New("invalid creation artifact")
		}
		if (e.Relationship == "attach" && a.ParentIssue != ref.Issue) || (e.Relationship == "linked" && a.ParentIssue != 0) {
			return nil, "", errors.New("creation relationship does not match reviewed source")
		}
		for _, target := range a.LinkIssues {
			if target < 1 {
				return nil, "", errors.New("invalid creation relationship target")
			}
		}
	}
	data, err := json.Marshal(e)
	if err != nil {
		return nil, "", err
	}
	hash := sha256.Sum256(data)
	return data, hex.EncodeToString(hash[:]), nil
}
