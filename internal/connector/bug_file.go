package connector

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"
)

// bugFileProposeTool is the dedicated proposal tool for the bug_file action.
// It is an action tool: it shares the action scope, guidance, failure texts
// and the owner-action Propose path with action_propose.
const bugFileProposeTool = "bug_file_propose"

// bugFileAction is the single catalogue name both routes propose.
const bugFileAction = "bug_file"

const (
	bugFileRepositoryMax  = 1024
	bugFileTitleMaxRunes  = 256
	bugFileDescriptionMax = 8192
	bugFileRequestKeyMax  = 128
)

var bugFileFields = []string{"repository", "title", "description", "client_request_key"}

func bugFileInputSchema() map[string]any {
	str := func(maximum int) map[string]any {
		return map[string]any{"type": "string", "minLength": 1, "maxLength": maximum}
	}
	return map[string]any{"type": "object", "properties": map[string]any{
		"repository":         str(bugFileRepositoryMax),
		"title":              str(bugFileTitleMaxRunes),
		"description":        str(bugFileDescriptionMax),
		"client_request_key": str(bugFileRequestKeyMax),
	}, "required": bugFileFields, "additionalProperties": false}
}

const bugFileDescription = "Propose filing a bug on a registered GitHub or Linear project for the owner, without filing it yet. Returns the same proposal as action_propose for the bug_file action; confirm it with action_confirm."

// parseBugFileArguments bounds the four strict fields and returns the very
// actionArguments that action_propose holds for action bug_file. The strings
// are kept as sent: the domain handler validates and normalises after the
// request digest, so both routes share one key and one proposal.
func parseBugFileArguments(raw json.RawMessage) (actionArguments, error) {
	var a actionArguments
	d := json.NewDecoder(bytes.NewReader(raw))
	if err := uniqueJSONObject(d); err != nil {
		return a, uniqueArgumentsError(err)
	}
	if _, err := d.Token(); err != io.EOF {
		return a, argumentsShapeError()
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return a, argumentsShapeError()
	}
	if err := checkArgumentFields(fields, bugFileFields, bugFileFields); err != nil {
		return a, err
	}
	var in struct {
		Repository       string `json:"repository"`
		Title            string `json:"title"`
		Description      string `json:"description"`
		ClientRequestKey string `json:"client_request_key"`
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		return a, decodeArgumentError(err)
	}
	for _, f := range []struct {
		field, value string
		ok           bool
		problem      string
	}{
		{"repository", in.Repository, len(in.Repository) <= bugFileRepositoryMax, "is longer than 1024 bytes"},
		{"title", in.Title, utf8.RuneCountInString(in.Title) <= bugFileTitleMaxRunes, "is longer than 256 characters"},
		{"description", in.Description, len(in.Description) <= bugFileDescriptionMax, "is longer than 8192 bytes"},
		{"client_request_key", in.ClientRequestKey, len(in.ClientRequestKey) <= bugFileRequestKeyMax, "is longer than 128 bytes"},
	} {
		if strings.TrimSpace(f.value) == "" {
			return a, invalidArgument(f.field, "must not be empty")
		}
		if !f.ok {
			return a, invalidArgument(f.field, f.problem)
		}
	}
	return actionArguments{
		Action:           bugFileAction,
		Arguments:        map[string]string{"repository": in.Repository, "title": in.Title, "description": in.Description},
		ClientRequestKey: in.ClientRequestKey,
	}, nil
}
