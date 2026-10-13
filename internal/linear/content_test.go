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
