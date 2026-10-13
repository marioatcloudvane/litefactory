package linear

import (
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
		strings.Replace(footerStoryFixture, `"lines":4`, `"lines":5`, 1),
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
