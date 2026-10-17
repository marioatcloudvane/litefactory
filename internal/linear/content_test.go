package linear

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/report"
	"os"
	"strings"
	"testing"
)

func TestProviderRepresentationPreservesContractAndOwnerBytes(t *testing.T) {
	for _, body := range []string{"owner prose \n\n<!-- lf:plan-story parent=abc key=story-a -->\n<!-- lf:plan-content v=1 data={\"key\":\"story-a\",\"notes\":[\"a & b\"]} -->\n", "<!-- litefactory:scope:task:1:story:freeze -->", "outside <!-- lf:owner-inline --> text", "```ordinary\n<!-- not-factory -->\n```\n", "```html\n<!-- lf:owner-example -->\n```\n", "````markdown\n```\n<!-- lf:owner-example -->\n```\n````\n"} {
		encoded := EncodeContent(body)
		decoded, err := DecodeContent(encoded)
		if err != nil || decoded != body {
			t.Fatalf("round trip changed content: %q %v", decoded, err)
		}
	}
	encoded := EncodeContent("<!-- lf:plan-content v=1 data={} -->")
	if strings.Contains(encoded, "<!--") || !strings.Contains(encoded, relocatedMetadataFence) {
		t.Fatal("metadata remains invisible HTML")
	}
	for _, body := range []string{metadataFence + "\nlf:marker", metadataFence + "\nowner altered metadata\n```"} {
		if _, err := DecodeContent(body); err == nil {
			t.Fatal("unexplained representation accepted")
		}
	}
}

func TestContentEquivalent(t *testing.T) {
	for _, tt := range []struct {
		name, expected, observed string
		equivalent               bool
	}{
		{"line endings", "hello\r\nworld", "hello\nworld", true},
		{"trailing space", "hello \nworld", "hello\nworld", true},
		{"table", "| A | B |\n| --- | :---: |\n| one | two |", "|A|B|\n|------|:-----:|\n|one|two|", true},
		{"escapes", `Hello\! Done\. Yes\?`, "Hello! Done. Yes?", true},
		{"prose", "hello", "goodbye", false},
		{"cell", "| A | B |\n| --- | --- |\n| one | two |", "| A | B |\n| --- | --- |\n| one | edited |", false},
		{"alignment", "| A |\n| --- |", "| A |\n| :--- |", false},
		{"bare www link", `www\.example.com`, "www.example.com", false},
		{"bare link", `https://example\.com`, "https://example.com", false},
		{"cell nonbreaking space", "| A |\n| --- |\n|\u00a0one|", "| A |\n| --- |\n|one|", false},
		{"link", "[x](https://one.test)", "[x](https://two.test)", false},
		{"code", "```go\na := 1 \n```", "```go\na := 1\n```", false},
		{"inline code", "`a `", "`a`", false},
		{"multiline code", "`a\nb \nc`", "`a\nb\nc`", false},
		{"indented code", "    a \n", "    a\n", false},
		{"list", "- a\n  - b", "- a\n- b", false},
		{"ordered list", "1\\. a", "1. a", false},
		{"hard break", "hello  \nworld", "hello\nworld", false},
		{"escaped space", "hello\\ \nworld", "hello\\\nworld", false},
		{"escaped structure", `a\*b\*`, "a*b*", false},
		{"escaped backslash", `hello\\!`, `hello\!`, false},
		{"metadata", "<!-- lf:plan-content v=1 data={} -->", "<!-- lf:plan-content v=1 data={ } -->", false},
		{"marker", "<!-- lf:plan-story key=a -->", "<!-- lf:plan-story key=b -->", false},
		{"malformed table", "| A | B |\n| --- | x |", "|A|B|\n|---|x|", false},
		{"html", "<pre>\na \n</pre>", "<pre>\na\n</pre>", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if ContentEquivalent(tt.expected, tt.observed) != tt.equivalent || ContentEquivalent(tt.observed, tt.expected) != tt.equivalent {
				t.Fatal("unexpected equivalence")
			}
		})
	}
	body := "<!-- lf:plan-content v=1 data={} -->\r\nprose\r\n"
	got, err := DecodeContent(EncodeContent(body))
	if err != nil || got != body {
		t.Fatalf("CRLF metadata roundtrip = %q, %v", got, err)
	}
}

// The expected body is reconstructed, never a claimed captured 21:19 write.
func mar12Pair(t *testing.T) (string, string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/mar-12-readback.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 3304 || fmt.Sprintf("%x", sha256.Sum256(raw)) != "98e141564f22ff4265dbb84bded3e2c495c457b803c9ad08c9755029e837a88b" {
		t.Fatal("live fixture changed")
	}
	observed, err := DecodeContent(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "<!-- lf:plan-content v=1 data="
	_, tail, ok := strings.Cut(observed, prefix)
	if !ok {
		t.Fatal("missing draft")
	}
	payload, _, ok := strings.Cut(tail, " -->")
	if !ok {
		t.Fatal("missing payload end")
	}
	var draft planbrief.PlanDraft
	if err = json.Unmarshal([]byte(payload), &draft); err != nil {
		t.Fatal(err)
	}
	_, body, err := report.RenderPlanDraft(4503599627567323, 1, draft, "story", planbrief.ContentScope{}, "MAR-11")
	if err != nil {
		t.Fatal(err)
	}
	expected := "<!-- lf:plan-story parent=4503599627567323 key=story-001 -->\n<!-- lf:plan-request digest=ed786d383bf1c84a31dcd0268555de20fb607414cc5d06da8cbaf0ff44b97885 -->\n" + body
	return expected, observed
}

func TestMAR12Readback(t *testing.T) {
	expected, observed := mar12Pair(t)
	if expected == observed {
		t.Fatal("fixture must exercise real representation differences")
	}
	if !ContentEquivalent(expected, observed) {
		t.Fatalf("comparison differs:\nexpected %s\nobserved %s", comparableContent(expected), comparableContent(observed))
	}
}

func TestContentListStructure(t *testing.T) {
	for _, tt := range []struct {
		name, a, b string
		equal      bool
	}{
		{"star", "- one\n- two", "* one\n* two", true},
		{"plus", "- one\n- two", "+ one\n+ two", true},
		{"nested", "- one\n  - sub\n- two", "* one\n  + sub\n* two", true},
		{"nesting changed", "- one\n  - sub", "* one\n* sub", false},
		{"grouping", "- one\n- two", "* one\n+ two", false},
		{"loose list is stored tight by Linear", "- one\n\n- two", "* one\n* two", true},
		{"paragraph", "one\n\ntwo", "one\ntwo", false},
		{"ordered unchanged", "2. one\n3. two", "2. one\n3. two", true},
		{"ordered start", "1. one\n2. two", "2. one\n3. two", false},
		{"ordered order", "1. one\n2. two", "1. two\n2. one", false},
		{"ordered conversion", "1. one", "* one", false},
		{"emphasis unchanged", "- **one**", "* **one**", true},
		{"emphasis removed", "- **one**", "* one", false},
		{"emphasis style unknown", "- **one**", "* __one__", false},
		{"literal punctuation", `- \*one\*`, "* *one*", false},
		{"checkbox marker", "- [ ] one", "* [ ] one", true},
		{"checkbox state", "- [ ] one", "* [x] one", false},
		{"code bullets", "```\n- one\n```", "```\n* one\n```", false},
		{"hard break", "- one  \n  two", "* one\n  two", false},
		{"rule not list", "- - -", "* * *", false},
		{"two dash table", "| A |\n| --- |", "|A|\n|--|", true},
		{"one dash unknown", "| A |\n| --- |", "|A|\n|-|", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if ContentEquivalent(tt.a, tt.b) != tt.equal || ContentEquivalent(tt.b, tt.a) != tt.equal {
				t.Fatal("unexpected comparison")
			}
		})
	}
}

func TestScaffoldEntities(t *testing.T) {
	expected, _ := mar12Pair(t)
	wrap := func(prose string) string {
		start := strings.Index(expected, "As invoice reviewer")
		end := strings.Index(expected[start:], "\n") + start
		return expected[:start] + prose + expected[end:]
	}
	for _, tt := range []struct {
		a, b  string
		equal bool
	}{
		{"Plain prose&#46; Next sentence&#46;", "Plain prose. Next sentence.", true},
		{"A layout&#45;only change", "A layout-only change", true},
		{"Text &#40;separate story&#41;", "Text (separate story)", true},
		{"- Changing the Open and Closed filters or the Ignore action &#40;story&#45;001&#41;", "- Changing the Open and Closed filters or the Ignore action (story-001)", true},
		{"1&#46; item", "1. item", true},
		{"1&#41; item", "1) item", true},
		{"&#45; item", "- item", true},
		{"&#45;&#45;&#45;", "---", true},
		{"Text [link]&#40;url&#41;", "Text [link](url)", true},
		{"Text ![image]&#40;url&#41;", "Text ![image](url)", true},
		{"<https://example&#46;com>", "<https://example.com>", false},
		{"Text www&#46;example.com", "Text www.example.com", true},
		{"`Text&#46;`", "`Text.`", false},
		{"```\nText&#46;\n```", "```\nText.\n```", false},
		{"    Text&#46;", "    Text.", false},
		{`Text\&#46;`, `Text\.`, true},
		{"Text&#46;", `Text\.`, false},
		{"Text&amp;#46;", "Text&#46;", false},
		{"Text&period;", "Text.", true},
		{"Text&#x2e;", "Text.", true},
		{"Text&unknown;", "Text.", false},
		{"Text&#46;", "Text!", false},
		{"Text **bold&#46;**", "Text **bold.**", true},
		{"Text\n\n&#45; item", "Text\n\n- item", true},
	} {
		t.Run(tt.a, func(t *testing.T) {
			a, b := wrap(tt.a), wrap(tt.b)
			if ContentEquivalent(a, b) != tt.equal || ContentEquivalent(b, a) != tt.equal {
				t.Fatal("unexpected entity equivalence")
			}
		})
	}
	if ContentEquivalent("Plain&#46;", "Plain.") {
		t.Fatal("decoded outside scaffold")
	}
	story := func(body string) string {
		return "<!-- lf:plan-story parent=1 key=story-001 -->\n<!-- lf:plan-request digest=" + strings.Repeat("a", 64) + " -->\n# Task\n\n" + body + "\n"
	}
	mar24 := "- [ ] AC-003: &#40;Manual&#41; The server lets POST /matches/no&#45;invoice&#45;expected with expected=false turn only a no&#95;invoice&#95;expected line into missing&#95;invoice; …"
	mar24Observed := "- [ ] AC-003: (Manual) The server lets POST /matches/no-invoice-expected with expected=false turn only a no_invoice_expected line into missing_invoice; …"
	if !ContentEquivalent(story(mar24), story(mar24Observed)) || !ContentEquivalent(story(mar24Observed), story(mar24)) {
		t.Fatal("MAR-24 checklist line differs")
	}
	references := []struct{ encoded, decoded string }{
		{"no&#95;invoice &#40;x&#41; &#42;", "no_invoice (x) *"},
		{"no&#x5f;invoice &#x28;x&#x29; &#x2A;", "no_invoice (x) *"},
		{"a &amp; b &quot;d&quot; &#39;e&#39;", `a & b "d" 'e'`},
		// A raw "<" outside a table falls back to byte comparison (a separate,
		// excluded issue), so &lt; and &gt; are shown equal to their numeric form.
		{"&lt;c&gt;", "&#60;c&#x3E;"},
	}
	shapes := []struct {
		name string
		line func(string) string
	}{
		{"prose", func(s string) string { return "Text " + s }},
		{"list", func(s string) string { return "- Item " + s }},
		{"checklist", func(s string) string { return "- [ ] AC-001: " + s }},
		{"table", func(s string) string { return "| ID | Text |\n|---|---|\n| C-1 | " + s + " |" }},
	}
	for _, shape := range shapes {
		for _, ref := range references {
			a, b := story(shape.line(ref.encoded)), story(shape.line(ref.decoded))
			if !ContentEquivalent(a, b) || !ContentEquivalent(b, a) {
				t.Fatalf("%s %q not equivalent to %q", shape.name, ref.encoded, ref.decoded)
			}
		}
	}
	if a, b := story(shapes[3].line("&lt;c&gt;")), story(shapes[3].line("<c>")); !ContentEquivalent(a, b) || !ContentEquivalent(b, a) {
		t.Fatal("table &lt; &gt; not decoded")
	}
	if ContentEquivalent(expected, strings.Replace(expected, `"frontend":true`, `"frontend":false`, 1)) {
		t.Fatal("metadata edit accepted")
	}
}

const legacyStoryFixture = "```litefactory-metadata-v1\nlf:plan-story parent=123 key=story-001\n```\n```litefactory-metadata-v1\nlf:plan-request digest=abc\n```\nStory.\n```litefactory-metadata-v1\nlf:delivery v=1 key=def\n```"
const canonicalStoryFixture = "<!-- lf:plan-story parent=123 key=story-001 -->\n<!-- lf:plan-request digest=abc -->\nStory.\n<!-- lf:delivery v=1 key=def -->"
const footerStoryFixture = "Story.\n\n+++ Factory metadata\n```litefactory-metadata-v2\n" + `{"lines":4,"markers":[{"line":0,"value":"lf:plan-story parent=123 key=story-001"},{"line":1,"value":"lf:plan-request digest=abc"},{"line":3,"value":"lf:delivery v=1 key=def"}]}` + "\n```\n+++"

func TestMetadataFooterFixtures(t *testing.T) {
	if Representation != "linear-markdown/v1" {
		t.Fatal("ledger identity changed")
	}
	for _, raw := range []string{legacyStoryFixture, footerStoryFixture} {
		got, err := DecodeContent(raw)
		if err != nil || got != canonicalStoryFixture {
			t.Fatalf("decode = %q, %v", got, err)
		}
	}
	if got := EncodeContent(canonicalStoryFixture); got != footerStoryFixture {
		t.Fatalf("encode = %q", got)
	}
	for _, body := range []string{
		canonicalStoryFixture,
		"Comment.\n<!-- lf:delivery v=1 key=def -->",
		"<!-- litefactory:scope:task:1:story:freeze -->",
		"<!-- lf:plan-content v=1 data={} -->\n<!-- lf:plan-request digest=abc -->",
		"Before\n<!-- lf:delivery key=a -->\nBetween\n<!-- lf:plan-content v=1 data={} -->\nAfter",
		"Example <!-- lf:inline --> text\n```html\n<!-- lf:example -->\n```\n~~~\n<!-- lf:example -->\n~~~",
		"+++ Factory metadata\nAn ordinary user section\n+++\n<!-- lf:delivery key=a -->",
		"````\n" + footerStoryFixture + "\n````\n<!-- lf:delivery key=a -->",
	} {
		for _, ending := range []string{"", "\n", "\n\n"} {
			for _, eol := range []string{"\n", "\r\n"} {
				original := strings.ReplaceAll(body+ending, "\n", eol)
				encoded := EncodeContent(original)
				decoded, err := DecodeContent(encoded)
				if err != nil || decoded != original {
					t.Fatalf("roundtrip %q = %q, %v", original, decoded, err)
				}
			}
		}
	}
	if !strings.HasPrefix(footerStoryFixture, "Story.") || strings.Count(footerStoryFixture, metadataFooter) != 1 || strings.Contains(strings.Split(footerStoryFixture, metadataFooter)[0], "lf:") {
		t.Fatal("markers precede prose or escape the single footer")
	}
}

func TestMetadataFooterRejectsDamage(t *testing.T) {
	for _, raw := range []string{
		strings.TrimSuffix(footerStoryFixture, "+++"),
		strings.Replace(footerStoryFixture, `"line":1`, `"line":0`, 1),
		strings.Replace(footerStoryFixture, `"line":1`, `"line":-1`, 1),
		strings.Replace(footerStoryFixture, `"line":3`, `"line":40`, 1),
		strings.Replace(footerStoryFixture, `"lines":4`, `"lines":4,"extra":true`, 1),
		strings.Replace(footerStoryFixture, `"lines":4`, `"lines":4,"lines":4`, 1),
		strings.Replace(footerStoryFixture, "lf:delivery", "owner:delivery", 1),
		strings.Replace(footerStoryFixture, metadataFooter, "+++ Other", 1),
		footerStoryFixture + "\nowner text",
		footerStoryFixture + "\n" + footerStoryFixture,
		legacyStoryFixture + "\n" + footerStoryFixture,
	} {
		if _, err := DecodeContent(raw); err == nil {
			t.Fatalf("accepted damaged footer: %q", raw)
		}
	}
}

func TestStoryReadbackCheckboxAndLinks(t *testing.T) {
	story := func(body string) string {
		return "<!-- lf:plan-story parent=1 key=story-001 -->\n<!-- lf:plan-request digest=" + strings.Repeat("a", 64) + " -->\n# Task\n\n" + body + "\n"
	}
	const mdn = "https://developer.mozilla.org/en-US/docs/Web/API/HTMLElement/focus"
	const tick = "AC-002: (Auto) The invoices table has no Source column: …"
	for _, tt := range []struct {
		name, a, b string
		equal      bool
	}{
		{"checked case", "- [x] " + tick, "- [X] " + tick, true},
		{"checked state", "- [ ] " + tick, "- [x] " + tick, false},
		{"checked state upper", "- [ ] " + tick, "- [X] " + tick, false},
		{"fenced checkbox", "```\n- [x] " + tick + "\n```", "```\n- [X] " + tick + "\n```", false},
		{"bare url link", "Checked MDN focus documentation &#40;[" + mdn + "](<" + mdn + ">)&#41;: ok&#46;", "Checked MDN focus documentation ([" + mdn + "](<" + mdn + ">)): ok.", true},
		{"markdown link", "See [LiteLLM JSON mode documentation](<https://docs.litellm.ai/docs/completion/json_mode>)&#46;", "See [LiteLLM JSON mode documentation](<https://docs.litellm.ai/docs/completion/json_mode>).", true},
		{"mangled link", "Checked MDN focus documentation &#40;[" + mdn + "](<" + mdn + ">)&#41;: ok&#46;", "Checked MDN focus documentation ([https://developer](<https://developer>).mozilla.org/en-US/docs/Web/API/HTMLElement/focus): ok.", false},
		{"link target changed", "See [a](<https://a.test/x>)&#46;", "See [a](<https://a.test/y>).", false},
		{"other angle bracket", "Text [a](<https://a.test>) &lt;b&gt;&#46;", "Text [a](<https://a.test>) <b>.", false},
		{"html still raw", "Text <b>x&#46;</b>", "Text <b>x.</b>", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a, b := story(tt.a), story(tt.b)
			if ContentEquivalent(a, b) != tt.equal || ContentEquivalent(b, a) != tt.equal {
				t.Fatalf("unexpected equivalence:\n%q\n%q", comparableContent(a), comparableContent(b))
			}
		})
	}
	// The story renderer's own output for the diagnostic lines reads back equal
	// to the decoded form Linear stores.
	s := planbrief.TypedStory{Key: "US-001", Intent: "create", Title: "Links",
		UserStory:          planbrief.UserStoryContent{Actor: "owner", Want: "links", Benefit: "they work", Notes: []string{"Checked MDN focus documentation (" + mdn + "): ok."}},
		AcceptanceCriteria: []planbrief.CriterionContent{{ID: "AC-001", Text: "See [LiteLLM JSON mode documentation](https://docs.litellm.ai/docs/completion/json_mode)."}},
		Tasks:              []planbrief.TaskContent{{ID: "T-001", Description: "Build it", OwnerRole: "engineer", CriterionIDs: []string{"AC-001"}, DependsOn: []string{}}},
		Architecture:       planbrief.ArchitectureContent{Summary: "Pure", Components: []planbrief.ComponentContent{{Name: "r", Responsibility: "k"}}, Interfaces: []planbrief.InterfaceContent{{Name: "r", Input: "i", Output: "o"}}, Constraints: []string{}, Decisions: []planbrief.DecisionContent{{Decision: "d", Reason: "r"}}},
		Dependencies:       planbrief.DependencyContent{StoryKeys: []string{}, IssueRefs: []string{}},
	}
	body, err := report.RenderTypedStory(s, planbrief.ContentScope{Roles: []string{"engineer"}})
	if err != nil {
		t.Fatal(err)
	}
	expected := story(body)
	var stored []string
	for _, line := range strings.Split(expected, "\n") {
		if !strings.HasPrefix(line, "<!--") && !strings.HasPrefix(line, "|") {
			line = decodeEntities(line)
		}
		stored = append(stored, line)
	}
	if !ContentEquivalent(expected, strings.Join(stored, "\n")) {
		t.Fatalf("rendered links differ:\n%s\n---\n%s", comparableContent(expected), comparableContent(strings.Join(stored, "\n")))
	}
}
