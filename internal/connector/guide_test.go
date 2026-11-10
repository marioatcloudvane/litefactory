package connector

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// withoutGuide drops the always-available guide from a tool list.
func withoutGuide(names []string) []string {
	return slices.DeleteFunc(slices.Clone(names), func(n string) bool { return n == guideTool })
}

// allToolNames lists every tool that has a guidance file.
func allToolNames(t *testing.T) []string {
	t.Helper()
	entries, err := fs.ReadDir(guidanceFiles, "guidance")
	if err != nil {
		t.Fatal(err)
	}
	var tools []string
	for _, e := range entries {
		if n, ok := strings.CutSuffix(e.Name(), ".md"); ok && !e.IsDir() {
			tools = append(tools, n)
		}
	}
	return tools
}

func namesTool(text, tool string) bool {
	return regexp.MustCompile(`(^|[^a-z_])` + regexp.QuoteMeta(tool) + `([^a-z_]|$)`).MatchString(text)
}

// Different grants get guidance that never names a tool they cannot call.
func TestGuideNeverNamesUncallableTool(t *testing.T) {
	t.Parallel()
	all := allToolNames(t)
	for _, tc := range []struct {
		name  string
		tools []string
	}{
		{"guide only", []string{guideTool}},
		{"overview", []string{guideTool, "factory_overview"}},
		{"discussion", []string{guideTool, "task_evidence_read"}},
		{"actions", append([]string{guideTool}, actionToolNames...)},
		{"workflows", append(append([]string{guideTool}, actionToolNames...), workflowToolNames...)},
		{"brainstorm", append([]string{guideTool}, brainstormToolNames...)},
		{"brainstorm and actions", append(append([]string{guideTool}, actionToolNames...), brainstormToolNames...)},
		{"usage", []string{guideTool, "usage_read"}},
		{"advisory", append([]string{guideTool}, advisoryToolNames...)},
		{"code review", append([]string{guideTool}, codeToolNames...)},
		{"everything", all},
	} {
		for _, topic := range []string{"", "overview", "workflows", "tools", "scopes"} {
			text, err := composeGuide(guidanceFiles, tc.tools, []string{"repository:actions"}, topic)
			if err != nil || strings.TrimSpace(text) == "" {
				t.Fatalf("%s/%q: %q %v", tc.name, topic, text, err)
			}
			for _, tool := range all {
				if namesTool(text, tool) && !slices.Contains(tc.tools, tool) {
					t.Errorf("%s/%q names %s, which this grant cannot call", tc.name, topic, tool)
				}
			}
		}
	}
}

// Every tool a section depends on is a tool the connector really has.
func TestGuideSectionsNameRealTools(t *testing.T) {
	t.Parallel()
	sections, err := readGuideSections(guidanceFiles)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range sections {
		for _, n := range append(slices.Clone(g.Needs), g.Missing...) {
			if !isKnownTool(n) {
				t.Errorf("section %q depends on unknown tool %s", g.Body[:20], n)
			}
			if _, err := fs.Stat(guidanceFiles, "guidance/"+n+".md"); err != nil {
				t.Errorf("tool %s has no guidance file", n)
			}
		}
	}
}

// The guide reuses the per-tool guidance files, so a change there reaches it.
func TestGuideReusesToolGuidance(t *testing.T) {
	t.Parallel()
	files := fstest.MapFS{}
	for _, n := range []string{"overview/factory.md", "overview/brainstorm.md", "overview/missing_scopes.md", "overview/scopes_complete.md"} {
		files["guidance/"+n] = &fstest.MapFile{Data: []byte("X {{scopes}}")}
	}
	files["guidance/guide/01.md"] = &fstest.MapFile{Data: []byte("topic: overview\n\n# Guide\n\nbody")}
	for _, marker := range []string{"FIRST-ONE", "FIRST-TWO"} {
		files["guidance/usage_read.md"] = &fstest.MapFile{Data: []byte(marker + " paragraph\n\nsecond paragraph")}
		files["guidance/factory_guide.md"] = &fstest.MapFile{Data: []byte("guide tool text")}
		text, err := composeGuide(files, []string{guideTool, "usage_read"}, nil, "")
		if err != nil || !strings.Contains(text, marker+" paragraph") || strings.Contains(text, "second paragraph") {
			t.Fatalf("%s: %q %v", marker, text, err)
		}
		full, err := composeGuide(files, []string{guideTool, "usage_read"}, nil, "usage_read")
		if err != nil || full != marker+" paragraph\n\nsecond paragraph" {
			t.Fatalf("tool topic: %q %v", full, err)
		}
	}
}

func TestGuideTopicsRefuseUncallableAndUnknown(t *testing.T) {
	t.Parallel()
	if _, err := composeGuide(guidanceFiles, []string{guideTool}, nil, "action_propose"); !errors.Is(err, errGuideTopic) {
		t.Fatalf("a tool the grant cannot call is not a topic: %v", err)
	}
	if _, err := composeGuide(guidanceFiles, []string{guideTool}, nil, "nonsense"); !errors.Is(err, errGuideTopic) {
		t.Fatalf("unknown topic: %v", err)
	}
}

func TestGuideSizeLimit(t *testing.T) {
	t.Parallel()
	build := func(pad int) fstest.MapFS {
		files := fstest.MapFS{
			"guidance/overview/factory.md":         {Data: []byte("O")},
			"guidance/overview/brainstorm.md":      {Data: []byte("B")},
			"guidance/overview/missing_scopes.md":  {Data: []byte("M {{scopes}}")},
			"guidance/overview/scopes_complete.md": {Data: []byte("C")},
			"guidance/factory_guide.md":            {Data: []byte("G")},
		}
		for i := range 4 {
			files["guidance/guide/0"+string(rune('1'+i))+".md"] = &fstest.MapFile{Data: []byte("topic: overview\n\n" + strings.Repeat("a", 7000))}
		}
		files["guidance/guide/09.md"] = &fstest.MapFile{Data: []byte("topic: overview\n\n" + strings.Repeat("a", pad))}
		return files
	}
	base, err := composeGuide(build(1), []string{guideTool}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	pad := 1 + maxGuideBytes - len(base)
	if pad < 1 || pad > maxGuidanceBytes-20 {
		t.Fatalf("test setup: pad %d", pad)
	}
	if text, err := composeGuide(build(pad), []string{guideTool}, nil, ""); err != nil || len(text) != maxGuideBytes {
		t.Fatalf("at the limit: %d %v", len(text), err)
	}
	if _, err := composeGuide(build(pad+1), []string{guideTool}, nil, ""); err == nil {
		t.Fatal("above the limit must be refused")
	}
}

func TestGuideBrokenFilesFailClosed(t *testing.T) {
	t.Parallel()
	files := fstest.MapFS{"guidance/guide/01.md": {Data: []byte("no header")}, "guidance/overview/scopes_complete.md": {Data: []byte("C")}, "guidance/factory_guide.md": {Data: []byte("G")}}
	if _, err := composeGuide(files, []string{guideTool}, nil, ""); err == nil {
		t.Fatal("a section without header must be refused")
	}
	if _, err := composeGuide(fstest.MapFS{}, []string{guideTool}, nil, ""); err == nil {
		t.Fatal("missing guide files must be refused")
	}
}

func TestGuideArguments(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]string{"": "", "null": "", "{}": "", `{"topic":"tools"}`: "tools"} {
		got, err := parseGuideArguments(json.RawMessage(raw))
		if err != nil || got != want {
			t.Errorf("%q: %q %v", raw, got, err)
		}
	}
	for _, raw := range []string{`{"topic":""}`, `{"topic":1}`, `{"topic":"a","topic":"b"}`, `{"other":1}`, `[]`, `{"topic":null}x`, `{"topic":"` + strings.Repeat("a", 65) + `"}`} {
		if _, err := parseGuideArguments(json.RawMessage(raw)); err == nil {
			t.Errorf("%q accepted", raw)
		}
	}
}

// The instructions stay short; the detail is in the guide.
func TestInstructionsAreShort(t *testing.T) {
	t.Parallel()
	all := allToolNames(t)
	text, err := composeInstructions(guidanceFiles, all, []string{"task:advisory"})
	if err != nil {
		t.Fatal(err)
	}
	if len(text) > 6000 || !strings.Contains(text, guideTool) {
		t.Fatalf("instructions: %d bytes", len(text))
	}
	// The overview itself names no tool but the guide, so it cannot promise an uncallable one.
	overview, _ := readOverview(guidanceFiles)
	for _, tool := range all {
		if tool != guideTool && namesTool(overview, tool) {
			t.Errorf("the overview names %s", tool)
		}
	}
}

func TestSkillIsBuiltFromTheSharedGuidance(t *testing.T) {
	t.Parallel()
	skill, err := ComposeSkill(guidanceFiles)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(skill, "---\n") {
		t.Fatalf("skill front matter: %q", skill[:60])
	}
	overview, _ := readOverview(guidanceFiles)
	sections, _ := readGuideSections(guidanceFiles)
	if !strings.Contains(skill, overview) {
		t.Fatal("skill must carry the shared instructions")
	}
	for _, g := range sections {
		has := strings.Contains(skill, g.Body)
		if len(g.Missing) > 0 && has {
			t.Errorf("skill carries an unavailable-only section: %.40s", g.Body)
		}
		if len(g.Missing) == 0 && !has {
			t.Errorf("skill lacks a section: %.40s", g.Body)
		}
	}
}

// Real client path: initialize, list the tools, then ask the guide, for grants
// that can and cannot reach the brainstorm tools.
func TestGuideClientPath(t *testing.T) {
	t.Parallel()
	_, b := missingScopeServer(t, true)
	call := func(token string, id int, method string, params any) map[string]any {
		t.Helper()
		body := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
		if params != nil {
			body["params"] = params
		}
		rec := b.rpc(t, token, body)
		var out map[string]any
		connectorDecode(t, rec, &out)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: HTTP %d %s", method, rec.Code, rec.Body.String())
		}
		return out
	}
	guide := func(token string, args map[string]any) (string, map[string]any) {
		t.Helper()
		out := call(token, 9, "tools/call", map[string]any{"name": guideTool, "arguments": args})
		result, _ := out["result"].(map[string]any)
		content, _ := result["content"].([]any)
		if len(content) == 0 {
			return "", out
		}
		item, _ := content[0].(map[string]any)
		text, _ := item["text"].(string)
		return text, out
	}
	for _, tc := range []struct {
		name, scope string
		brainstorm  bool
	}{
		{"without brainstorm", DiscussionScope + " " + AdvisoryScope, false},
		{"with brainstorm", BrainstormScope, true},
	} {
		token := b.token(t, tc.scope)
		init := call(token, 1, "initialize", map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "1"}})
		instructions, _ := init["result"].(map[string]any)["instructions"].(string)
		if !strings.Contains(instructions, guideTool) || len(instructions) > 6000 {
			t.Fatalf("%s: instructions must point to the guide and stay short (%d bytes)", tc.name, len(instructions))
		}
		listed := call(token, 2, "tools/list", nil)
		tools, _ := listed["result"].(map[string]any)["tools"].([]any)
		first, _ := tools[0].(map[string]any)
		annotations, _ := first["annotations"].(map[string]any)
		if first["name"] != guideTool || annotations["readOnlyHint"] != true || annotations["destructiveHint"] != false {
			t.Fatalf("%s: the guide must be listed first and read-only: %v", tc.name, first)
		}
		text, out := guide(token, map[string]any{})
		if strings.TrimSpace(text) == "" {
			t.Fatalf("%s: guide call: %v", tc.name, out)
		}
		if namesTool(text, "bug_file_propose") || namesTool(text, "action_confirm") {
			t.Fatalf("%s: the guide must be cut to this grant:\n%s", tc.name, text)
		}
		if !tc.brainstorm && namesTool(text, "brainstorm_open") {
			t.Fatalf("%s: must name the missing scope and not the tool:\n%s", tc.name, text)
		}
		if tc.brainstorm && !namesTool(text, "brainstorm_open") {
			t.Fatalf("%s: must describe the brainstorm tools it can call", tc.name)
		}
		short, _ := guide(token, map[string]any{"topic": "scopes"})
		if short == "" || len(short) >= len(text) {
			t.Fatalf("%s: a topic must be shorter than the whole guide: %q", tc.name, short)
		}
		if tc.brainstorm {
			if full, _ := guide(token, map[string]any{"topic": "brainstorm_open"}); full == "" {
				t.Fatalf("%s: a callable tool is a topic", tc.name)
			}
		}
		if _, refused := guide(token, map[string]any{"topic": "bug_file_propose"}); refused["error"] == nil {
			t.Fatalf("%s: a tool the grant cannot call is not a topic: %v", tc.name, refused)
		}
		if _, unknown := guide(token, map[string]any{"extra": true}); unknown["error"] == nil {
			t.Fatalf("%s: unknown arguments must be refused", tc.name)
		}
	}
}

// Every guide job's chain is named in its steps in order, offered to a
// connection that can call all of it, and withheld from one missing a tool.
func TestGuideJobsMapToToolChains(t *testing.T) {
	t.Parallel()
	sections, err := readGuideSections(guidanceFiles)
	if err != nil {
		t.Fatal(err)
	}
	jobs := 0
	for _, g := range sections {
		if g.Job == "" {
			continue
		}
		jobs++
		pos := -1
		for _, tool := range g.Chain {
			if !slices.Contains(g.Needs, tool) {
				t.Errorf("%q: chain tool %s is not in the section's needs", g.Job, tool)
			}
			at := strings.Index(g.Body, tool)
			if at < 0 || at < pos {
				t.Errorf("%q: the steps must name %s after the earlier tools of the chain", g.Job, tool)
			}
			pos = at
		}
		line := "- " + g.Job + ": " + strings.Join(g.Chain, ", then ")
		grant := append([]string{guideTool}, g.Needs...)
		ins, err := composeInstructions(guidanceFiles, grant, nil)
		if err != nil || !strings.Contains(ins, line) {
			t.Errorf("%q: instructions lack %q: %v", g.Job, line, err)
		}
		for _, drop := range g.Needs {
			short := slices.DeleteFunc(slices.Clone(grant), func(n string) bool { return n == drop })
			ins, err := composeInstructions(guidanceFiles, short, nil)
			if err != nil || strings.Contains(ins, line) {
				t.Errorf("%q: job offered without %s: %v", g.Job, drop, err)
			}
		}
	}
	if jobs == 0 {
		t.Fatal("no guide section declares a job")
	}
}
