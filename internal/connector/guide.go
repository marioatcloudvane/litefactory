package connector

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"slices"
	"strings"
)

// guideTool is the read-only tool that returns the guide. It is callable by
// every signed-in connection and files, confirms and approves nothing.
const guideTool = "factory_guide"

// maxGuideBytes bounds one guide reply.
const maxGuideBytes = 32 * 1024

var errGuideTopic = errors.New("unknown guide topic")

// guideSection is one markdown file under guidance/guide. Its header lines name
// its topic and the tools it depends on; the wording lives in the file.
type guideSection struct {
	Topic   string
	Needs   []string // shown only when every one of these tools is callable
	Missing []string // shown only when at least one of these tools is not callable
	Job     string   // one-line job name for the instructions' job map
	Chain   []string // the tools to start with, in order, for that job
	Body    string
}

func (g guideSection) visible(tools []string) bool {
	for _, n := range g.Needs {
		if !slices.Contains(tools, n) {
			return false
		}
	}
	if len(g.Missing) == 0 {
		return true
	}
	for _, n := range g.Missing {
		if !slices.Contains(tools, n) {
			return true
		}
	}
	return false
}

func readGuideSections(files fs.FS) ([]guideSection, error) {
	entries, err := fs.ReadDir(files, "guidance/guide")
	if err != nil {
		return nil, errGuidanceUnavailable
	}
	var out []guideSection
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		raw, err := readGuidanceFile(files, "guidance/guide/"+e.Name())
		if err != nil {
			return nil, err
		}
		head, body, ok := strings.Cut(raw, "\n\n")
		if !ok {
			return nil, errGuidanceUnavailable
		}
		var g guideSection
		for _, line := range strings.Split(head, "\n") {
			k, v, _ := strings.Cut(line, ": ")
			switch k {
			case "topic":
				g.Topic = strings.TrimSpace(v)
			case "needs":
				g.Needs = strings.Fields(v)
			case "missing":
				g.Missing = strings.Fields(v)
			case "job":
				g.Job = strings.TrimSpace(v)
			case "chain":
				g.Chain = strings.Fields(v)
			}
		}
		if (g.Topic != "overview" && g.Topic != "workflows") || strings.TrimSpace(body) == "" || (g.Job == "") != (len(g.Chain) == 0) {
			return nil, errGuidanceUnavailable
		}
		g.Body = strings.TrimSpace(body)
		out = append(out, g)
	}
	if len(out) == 0 {
		return nil, errGuidanceUnavailable
	}
	return out, nil
}

func allCallable(names, tools []string) bool {
	for _, n := range names {
		if !slices.Contains(tools, n) {
			return false
		}
	}
	return true
}

// jobMap lists, for the tools a grant can call, one line per job with the tools
// to call in order. A job whose tools are not all callable is left out.
func jobMap(files fs.FS, tools []string) (string, error) {
	sections, err := readGuideSections(files)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, g := range sections {
		if g.Job != "" && g.visible(tools) && allCallable(g.Chain, tools) {
			lines = append(lines, "- "+g.Job+": "+strings.Join(g.Chain, ", then "))
		}
	}
	return strings.Join(lines, "\n"), nil
}

// toolReference gives each callable tool's first guidance paragraph, cut at a sentence end. Tools that
// share the same text are listed together.
func toolReference(files fs.FS, tools []string) (string, error) {
	var order []string
	names := map[string][]string{}
	for _, tool := range tools {
		if !isKnownTool(tool) {
			return "", errGuidanceUnavailable
		}
		text, err := readGuidanceFile(files, "guidance/"+tool+".md")
		if err != nil {
			return "", err
		}
		first, _, _ := strings.Cut(text, "\n\n")
		first = shortParagraph(first, maxToolParagraph)
		if _, seen := names[first]; !seen {
			order = append(order, first)
		}
		names[first] = append(names[first], tool)
	}
	lines := make([]string, 0, len(order))
	for _, first := range order {
		lines = append(lines, "- **"+strings.Join(names[first], ", ")+"**: "+first)
	}
	return strings.Join(lines, "\n"), nil
}

// maxToolParagraph bounds one tool's entry in the guide's tools section; the
// full text of a tool is served by the guide's topic for that tool.
const maxToolParagraph = 420

// shortParagraph keeps whole sentences of p up to limit bytes, and at least the
// first sentence.
func shortParagraph(p string, limit int) string {
	p = strings.TrimSpace(p)
	if len(p) <= limit {
		return p
	}
	cut := 0
	for i := 0; i+1 < len(p) && i < limit; i++ {
		if p[i] == '.' && p[i+1] == ' ' {
			cut = i + 1
		}
	}
	if cut == 0 {
		if i := strings.Index(p, ". "); i >= 0 {
			return p[:i+1]
		}
		return p
	}
	return p[:cut]
}

func scopesText(files fs.FS, missingScopes []string) (string, error) {
	if len(missingScopes) == 0 {
		return readGuidanceFile(files, "guidance/overview/scopes_complete.md")
	}
	notice, err := readGuidanceFile(files, "guidance/overview/missing_scopes.md")
	if err != nil || !strings.Contains(notice, "{{scopes}}") {
		return "", errGuidanceUnavailable
	}
	return strings.ReplaceAll(notice, "{{scopes}}", strings.Join(missingScopes, ", ")), nil
}

// composeGuide builds the guide for the tools a grant can call. An empty topic
// is the whole guide; otherwise one part, or one tool's full guidance. It names
// only tools in the list.
func composeGuide(files fs.FS, tools, missingScopes []string, topic string) (string, error) {
	text, err := guideText(files, tools, missingScopes, topic, true)
	if err != nil {
		return "", err
	}
	if len(text) > maxGuideBytes {
		return "", errGuidanceUnavailable
	}
	return text, nil
}

func guideText(files fs.FS, tools, missingScopes []string, topic string, withScopes bool) (string, error) {
	if slices.Contains(tools, topic) {
		return readGuidanceFile(files, "guidance/"+topic+".md")
	}
	sections, err := readGuideSections(files)
	if err != nil {
		return "", err
	}
	part := func(t string) string {
		var bodies []string
		for _, g := range sections {
			if g.Topic == t && g.visible(tools) {
				bodies = append(bodies, g.Body)
			}
		}
		return strings.Join(bodies, "\n\n")
	}
	toolsPart := func() (string, error) {
		ref, err := toolReference(files, tools)
		if err != nil {
			return "", err
		}
		return "## Tools you can call\n\n" + ref, nil
	}
	scopesPart := func() (string, error) {
		t, err := scopesText(files, missingScopes)
		if err != nil {
			return "", err
		}
		if rest, ok := strings.CutPrefix(t, "# "); ok {
			t = "## " + rest
		} else {
			t = "## Consent scopes\n\n" + t
		}
		return t, nil
	}
	switch topic {
	case "overview", "workflows":
		return part(topic), nil
	case "tools":
		return toolsPart()
	case "scopes":
		return scopesPart()
	case "":
		parts := []string{part("overview"), part("workflows")}
		t, err := toolsPart()
		if err != nil {
			return "", err
		}
		parts = append(parts, t)
		if withScopes {
			s, err := scopesPart()
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, "\n\n"), nil
	}
	return "", errGuideTopic
}

// composeSkill prints a client-side skill file from the same guidance: a
// header, the short instructions, the brainstorm guide and the guide for every
// tool the factory has.
func composeSkill(files fs.FS) (string, error) {
	header, err := readGuidanceFile(files, "guidance/skill/header.md")
	if err != nil {
		return "", err
	}
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
	guide, err := guideText(files, tools, nil, "", false)
	if err != nil {
		return "", err
	}
	return header + "\n\n" + overview + "\n\n" + brainstorm + "\n\n" + guide + "\n", nil
}

// ComposeSkill returns the client-side skill file built from the embedded guidance.
func ComposeSkill(guidance fs.FS) (string, error) { return composeSkill(guidance) }

// parseGuideArguments accepts no arguments, an empty object, or one topic.
func parseGuideArguments(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return "", nil
	}
	if !noDuplicateKeys(trimmed) {
		return "", duplicateArgument()
	}
	var a struct {
		Topic *string `json:"topic"`
	}
	d := json.NewDecoder(bytes.NewReader(trimmed))
	d.DisallowUnknownFields()
	if err := d.Decode(&a); err != nil {
		return "", decodeArgumentError(err)
	}
	if d.More() {
		return "", argumentsShapeError()
	}
	if a.Topic == nil {
		return "", nil
	}
	if *a.Topic == "" || len(*a.Topic) > 64 {
		return "", invalidArgument("topic", "must be 1 to 64 characters")
	}
	return *a.Topic, nil
}
