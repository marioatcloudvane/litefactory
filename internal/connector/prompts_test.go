package connector

import (
	"strings"
	"testing"
	"testing/fstest"
)

func guideFS(guide string) fstest.MapFS {
	return fstest.MapFS{
		"guidance/overview/factory.md":           {Data: []byte("OVERVIEW")},
		"guidance/overview/brainstorm.md":        {Data: []byte(guide)},
		"guidance/usage_read.md":                 {Data: []byte("USAGE")},
		"guidance/prompts/brainstorm_feature.md": {Data: []byte("title: B\ndescription: d\n\nBSPECIFIC")},
		"guidance/prompts/whats_waiting.md":      {Data: []byte("title: W\ndescription: d\n\nWBODY")},
		"guidance/prompts/review_task.md":        {Data: []byte("title: R\ndescription: d\nargument: task\n\nReview {{task}}.")},
	}
}

func TestCompositionOrderAndGuideReachesAllSurfaces(t *testing.T) {
	t.Parallel()
	for _, guide := range []string{"GUIDE-ONE", "GUIDE-TWO"} {
		files := guideFS(guide)
		ins, err := composeInstructions(files, []string{"usage_read"})
		if err != nil || ins != "OVERVIEW\n\n"+guide+"\n\nUSAGE" {
			t.Fatalf("instructions: %q %v", ins, err)
		}
		_, text, err := promptText(files, promptSpecs[0], "")
		if err != nil || text != guide+"\n\nBSPECIFIC" {
			t.Fatalf("prompt: %q %v", text, err)
		}
		tpl, err := composeTemplate(files)
		if err != nil || strings.Count(tpl, guide) != 1 || !strings.Contains(tpl, "USAGE") || !strings.Contains(tpl, "WBODY") || !strings.Contains(tpl, "Review <task>.") {
			t.Fatalf("template: %q %v", tpl, err)
		}
	}
}

func TestSharedGuideFailsClosed(t *testing.T) {
	t.Parallel()
	bad := map[string]fstest.MapFS{"empty": guideFS("  \n"), "oversized": guideFS(strings.Repeat("x", 8193)), "missing": guideFS("x")}
	delete(bad["missing"], "guidance/overview/brainstorm.md")
	for name, files := range bad {
		if _, err := composeInstructions(files, nil); err == nil {
			t.Errorf("%s: instructions answered", name)
		}
		if _, _, err := promptText(files, promptSpecs[0], ""); err == nil {
			t.Errorf("%s: prompt answered", name)
		}
		if _, err := composeTemplate(files); err == nil {
			t.Errorf("%s: template answered", name)
		}
	}
}

func TestPromptListFollowsTools(t *testing.T) {
	t.Parallel()
	files := guideFS("G")
	count := func(tools ...string) int {
		l, err := promptList(files, tools)
		if err != nil {
			t.Fatal(err)
		}
		return len(l)
	}
	if count() != 1 || count("factory_overview") != 2 || count("factory_overview", "task_evidence_read") != 3 {
		t.Fatal("prompt list does not follow granted tools")
	}
	_, text, _ := promptText(files, promptSpecs[2], "42")
	if text != "Review 42." {
		t.Fatalf("argument not inserted: %q", text)
	}
}

func TestStrictParams(t *testing.T) {
	t.Parallel()
	var v struct {
		Name string `json:"name"`
	}
	for raw, want := range map[string]bool{`{"name":"a"}`: true, `{"name":"a","name":"b"}`: false, `{"name":"a","x":1}`: false, `[]`: false} {
		if strictParams([]byte(raw), &v) != want {
			t.Errorf("%s: want %v", raw, want)
		}
	}
}
