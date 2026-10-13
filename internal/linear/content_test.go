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
	if strings.Contains(encoded, "<!--") || !strings.Contains(encoded, metadataFence) {
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
		{"loose outside scaffold", "- one\n\n- two", "* one\n* two", false},
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
		{"1&#46; item", "1. item", false},
		{"1&#41; item", "1) item", false},
		{"&#45; item", "- item", false},
		{"&#45;&#45;&#45;", "---", false},
		{"Text [link]&#40;url&#41;", "Text [link](url)", false},
		{"Text ![image]&#40;url&#41;", "Text ![image](url)", false},
		{"<https://example&#46;com>", "<https://example.com>", false},
		{"Text www&#46;example.com", "Text www.example.com", false},
		{"`Text&#46;`", "`Text.`", false},
		{"```\nText&#46;\n```", "```\nText.\n```", false},
		{"    Text&#46;", "    Text.", false},
		{`Text\&#46;`, `Text\.`, false},
		{"Text&#46;", `Text\.`, false},
		{"Text&amp;#46;", "Text&#46;", false},
		{"Text&period;", "Text.", false},
		{"Text&#x2e;", "Text.", false},
		{"Text&unknown;", "Text.", false},
		{"Text&#46;", "Text!", false},
		{"Text **bold&#46;**", "Text **bold.**", false},
		{"Text\n\n&#45; item", "Text\n\n- item", false},
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
	if ContentEquivalent(expected, strings.Replace(expected, `"frontend":true`, `"frontend":false`, 1)) {
		t.Fatal("metadata edit accepted")
	}
}
