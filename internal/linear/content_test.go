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
