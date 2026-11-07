package connector

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"unicode/utf8"
)

const maxGuidanceBytes = 8192

var errGuidanceUnavailable = errors.New("guidance unavailable")

// promptSpec is the fixed table of prompts. The wording lives in the markdown
// files; this table only selects files, required tools and argument names.
type promptSpec struct {
	Name     string
	Tool     string // empty: always listed
	Argument string // empty: no argument
	Shared   bool   // prepend the shared brainstorm guide
}

var promptSpecs = []promptSpec{
	{Name: "brainstorm_feature", Shared: true},
	{Name: "whats_waiting", Tool: "factory_overview"},
	{Name: "review_task", Tool: "task_evidence_read", Argument: "task"},
}

// readGuidanceFile is the one bounded reader for every shared text.
func readGuidanceFile(files fs.FS, path string) (string, error) {
	b, err := fs.ReadFile(files, path)
	if err != nil || !utf8.Valid(b) || len(b) > maxGuidanceBytes {
		return "", errGuidanceUnavailable
	}
	t := strings.TrimSpace(string(b))
	if t == "" {
		return "", errGuidanceUnavailable
	}
	return t, nil
}

func readOverview(files fs.FS) (string, error) {
	return readGuidanceFile(files, "guidance/overview/factory.md")
}

// readBrainstormGuide is shared by instructions, the brainstorm prompt and the template.
func readBrainstormGuide(files fs.FS) (string, error) {
	return readGuidanceFile(files, "guidance/overview/brainstorm.md")
}

// composeInstructions is the short text every client reads first: the
// overview, the names of the tools this grant can call, then the notice naming
// consent scopes the grant lacks, when there are any. The full guidance is
// served by the guide tool, not repeated here.
func composeInstructions(files fs.FS, tools, missingScopes []string) (string, error) {
	overview, err := readOverview(files)
	if err != nil {
		return "", err
	}
	// Every callable tool must still have readable guidance, or initialize fails.
	if _, err := loadGuidance(files, tools); err != nil {
		return "", err
	}
	notice, err := readGuidanceFile(files, "guidance/overview/missing_scopes.md")
	if err != nil || !strings.Contains(notice, "{{scopes}}") {
		return "", errGuidanceUnavailable
	}
	parts := []string{overview}
	jobs, err := jobMap(files, tools)
	if err != nil {
		return "", err
	}
	if jobs != "" {
		parts = append(parts, "## What you can help with\n\nStart with these tools, in this order. Call factory_guide for the steps.\n\n"+jobs)
	}
	if len(tools) > 0 {
		parts = append(parts, "## Tools you can call\n\n"+strings.Join(tools, ", ")+".")
	}
	if len(missingScopes) > 0 {
		parts = append(parts, strings.ReplaceAll(notice, "{{scopes}}", strings.Join(missingScopes, ", ")))
	}
	return strings.Join(parts, "\n\n"), nil
}

type promptFile struct {
	Title, Description, Body string
}

func readPromptFile(files fs.FS, name string) (promptFile, error) {
	raw, err := readGuidanceFile(files, "guidance/prompts/"+name+".md")
	if err != nil {
		return promptFile{}, err
	}
	var pf promptFile
	head, body, ok := strings.Cut(raw, "\n\n")
	if !ok {
		return promptFile{}, errGuidanceUnavailable
	}
	for _, line := range strings.Split(head, "\n") {
		k, v, _ := strings.Cut(line, ": ")
		switch k {
		case "title":
			pf.Title = strings.TrimSpace(v)
		case "description":
			pf.Description = strings.TrimSpace(v)
		}
	}
	pf.Body = strings.TrimSpace(body)
	if pf.Title == "" || pf.Body == "" {
		return promptFile{}, errGuidanceUnavailable
	}
	return pf, nil
}

func (p promptSpec) listed(tools []string) bool {
	return p.Tool == "" || slices.Contains(tools, p.Tool)
}

// promptList returns the prompts a grant may use, in table order.
func promptList(files fs.FS, tools []string) ([]any, error) {
	out := []any{}
	for _, p := range promptSpecs {
		if !p.listed(tools) {
			continue
		}
		pf, err := readPromptFile(files, p.Name)
		if err != nil {
			return nil, err
		}
		entry := map[string]any{"name": p.Name, "title": pf.Title, "description": pf.Description}
		if p.Argument != "" {
			entry["arguments"] = []any{map[string]any{"name": p.Argument, "required": true}}
		}
		out = append(out, entry)
	}
	return out, nil
}

// promptText builds the message text of one prompt. The argument is data.
func promptText(files fs.FS, p promptSpec, arg string) (string, string, error) {
	pf, err := readPromptFile(files, p.Name)
	if err != nil {
		return "", "", err
	}
	text := pf.Body
	if p.Argument != "" {
		text = strings.ReplaceAll(text, "{{"+p.Argument+"}}", arg)
	}
	if p.Shared {
		guide, err := readBrainstormGuide(files)
		if err != nil {
			return "", "", err
		}
		text = guide + "\n\n" + text
	}
	return pf.Description, text, nil
}

// composeTemplate prints the owner's project template from the same files.
func composeTemplate(files fs.FS) (string, error) {
	overview, err := readOverview(files)
	if err != nil {
		return "", err
	}
	brainstorm, err := readBrainstormGuide(files)
	if err != nil {
		return "", err
	}
	entries, err := fs.ReadDir(files, "guidance")
	if err != nil {
		return "", errGuidanceUnavailable
	}
	var tools []string
	for _, e := range entries {
		if n, ok := strings.CutSuffix(e.Name(), ".md"); ok && !e.IsDir() {
			tools = append(tools, n)
		}
	}
	toolText, err := loadGuidance(files, tools)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("# Litefactory project template\n\n")
	b.WriteString(overview + "\n\n" + brainstorm + "\n\n## Tool guidance\n\n" + toolText + "\n\n## Prompts\n")
	for _, p := range promptSpecs {
		pf, err := readPromptFile(files, p.Name)
		if err != nil {
			return "", err
		}
		body := pf.Body
		if p.Argument != "" {
			body = strings.ReplaceAll(body, "{{"+p.Argument+"}}", "<"+p.Argument+">")
		}
		b.WriteString("\n### " + pf.Title + "\n\n" + body + "\n")
	}
	return b.String(), nil
}

// ComposeTemplate returns the project template built from the embedded guidance.
func ComposeTemplate() (string, error) { return composeTemplate(guidanceFiles) }

// strictParams decodes with unknown fields and duplicate keys refused.
func strictParams(raw json.RawMessage, v any) bool {
	if !noDuplicateKeys(raw) {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v) == nil && !dec.More()
}

func noDuplicateKeys(raw json.RawMessage) bool {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var walk func() bool
	walk = func() bool {
		t, err := dec.Token()
		if err != nil {
			return false
		}
		d, ok := t.(json.Delim)
		if !ok {
			return true
		}
		if d == '{' {
			seen := map[string]bool{}
			for dec.More() {
				k, err := dec.Token()
				if err != nil {
					return false
				}
				ks, _ := k.(string)
				if seen[ks] {
					return false
				}
				seen[ks] = true
				if !walk() {
					return false
				}
			}
		} else {
			for dec.More() {
				if !walk() {
					return false
				}
			}
		}
		_, err = dec.Token()
		return err == nil
	}
	return walk()
}
