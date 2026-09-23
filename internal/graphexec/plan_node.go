package graphexec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// PlanNode is the dedicated private Plan result, with no Research payload or
// agent-authored approval. Parent, issue targets and owner authority are absent.
type PlanNode struct {
	Version   int               `json:"version"`
	Response  string            `json:"response"`
	Request   *PlanStoryRequest `json:"request"`
	Comment   string            `json:"comment"`
	Questions []string          `json:"questions"`
}

// ParsePlanNode validates shape and stage semantics before retaining any output.
func ParsePlanNode(data []byte, scope planbrief.ContentScope) (PlanNode, error) {
	var node PlanNode
	if len(data) > 128*1024 || !utf8.Valid(data) {
		return node, errors.New("plan node exceeds 128 KiB or is not UTF-8")
	}
	if err := uniquePlanJSON(json.NewDecoder(bytes.NewReader(data)), 0); err != nil {
		return node, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&node); err != nil {
		return PlanNode{}, err
	}
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		return PlanNode{}, errors.New("trailing Plan node data")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return PlanNode{}, err
	}
	for _, name := range []string{"version", "response", "request", "comment", "questions"} {
		if _, ok := fields[name]; !ok {
			return PlanNode{}, fmt.Errorf("plan node missing %s", name)
		}
	}
	if node.Version != 1 || strings.TrimSpace(node.Comment) == "" || len(node.Comment) > 8192 || node.Questions == nil || len(node.Questions) > 32 {
		return PlanNode{}, errors.New("invalid Plan node version, comment or questions")
	}
	for _, q := range node.Questions {
		if strings.TrimSpace(q) == "" || len(q) > 2048 {
			return PlanNode{}, errors.New("invalid Plan question")
		}
	}
	switch node.Response {
	case "ask":
		if len(node.Questions) == 0 {
			return PlanNode{}, errors.New("plan ask requires questions")
		}
	case "review_ready":
		if len(node.Questions) != 0 || node.Request == nil {
			return PlanNode{}, errors.New("plan review requires candidate and no questions")
		}
	default:
		return PlanNode{}, errors.New("plan roles may return only ask or review_ready, never approval")
	}
	if node.Request != nil {
		if err := node.Request.Validate(); err != nil {
			return PlanNode{}, err
		}
		for _, section := range node.Request.ChangedSections {
			if node.Request.Stage == "story" && section != "intent" && section != "dependencies" && section != "conversation" {
				return PlanNode{}, errors.New("story role cannot author later-stage sections")
			}
			if node.Request.Stage == "technical" && (section == "criteria" || section == "tasks") {
				return PlanNode{}, errors.New("technical role cannot author acceptance sections")
			}
		}
		if node.Request.Operation == "create" {
			if err := node.Request.Draft.Validate(node.Request.Stage, scope); err != nil {
				return PlanNode{}, err
			}
		} else if err := node.Request.Draft.ValidateRetained(node.Request.Stage, scope); err != nil {
			return PlanNode{}, err
		}
	}
	return node, nil
}
