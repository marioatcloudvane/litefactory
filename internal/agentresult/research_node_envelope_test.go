package agentresult

import (
	"os"
	"strings"
	"testing"
)

// The fixture is the exact terminal text a live Claude run produced on
// issue #313: a sentence of commentary, then the required object inside a
// Markdown fence. The run had already investigated the request and durably
// submitted its result; only this echo was rejected, and the findings were
// lost with it. Codex emits a bare object, which is why nine earlier graph
// runs passed and this was not noticed until the first Claude one.
func TestDecodeResearchNodeAcceptsAFencedAnswer(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/live/claude-fenced-node-result.txt")
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(strings.TrimSpace(string(raw)), "{") {
		t.Fatal("the fixture must keep its prose and fence, or it tests nothing")
	}
	node, d := DecodeResearchNode(raw, Context{SchemaVersion: SchemaVersion, Mode: "research", WorkflowVersion: 2, QuestionsAllowed: true})
	if !d.Valid || !d.Complete {
		t.Fatalf("a fenced answer is still an answer: %+v", d.Diagnostics)
	}
	if node.Response != "done" || node.Result.Outcome != "completed" {
		t.Fatalf("decoded the wrong thing: response=%q outcome=%q", node.Response, node.Result.Outcome)
	}
	if node.Result.Payload.Research == nil || len(node.Result.Payload.Research.Findings) == 0 {
		t.Fatal("the findings inside the fence were dropped")
	}
}

// Narrowing must not become a way to smuggle an oversized result past the
// bound: the ceiling applies to what the agent emitted as well as to what is
// left after the prose is removed.
func TestDecodeResearchNodeBoundsTheRawTextToo(t *testing.T) {
	t.Parallel()
	body := `{"version":1,"response":"done","result":{"outcome":"completed","comment":"c","payload":{"research":{"request":"r"}}}}`
	oversized := []byte(strings.Repeat("x", MaxResearchNodeBytes) + "\n" + body)
	if _, d := DecodeResearchNode(oversized, Context{SchemaVersion: SchemaVersion, Mode: "research", WorkflowVersion: 2}); d.Valid {
		t.Error("an oversized preamble must not pass the bound by being narrowed away")
	}
}

// Narrowing is for presentation, not for shape. Text that already starts as
// JSON is judged as written: an array holding the right object is still the
// wrong answer, and hunting for braces inside one would accept it.
func TestDecodeResearchNodeNarrowsOnlyProse(t *testing.T) {
	t.Parallel()
	body := `{"version":1,"response":"done","result":{"outcome":"completed","comment":"c","payload":{"research":{"request":"r"}}}}`
	ctx := Context{SchemaVersion: SchemaVersion, Mode: "research", WorkflowVersion: 2}
	for name, raw := range map[string]string{
		"array root":  "[" + body + "]",
		"string root": `"not an object"`,
		// Trailing bytes after the object are refused rather than guessed at:
		// commentary and a second JSON value are indistinguishable without
		// deciding which is which, and one of them breaks the contract.
		"extra value": body + " true",
		"trailing":    body + "\n\nThat is all.",
	} {
		if _, d := DecodeResearchNode([]byte(raw), ctx); d.Valid {
			t.Errorf("%s: a document of the wrong shape was narrowed into the right one", name)
		}
	}
	for name, raw := range map[string]string{
		"fenced": "Here it is:\n\n```json\n" + body + "\n```",
		"prose":  "Returning the final response. " + body,
	} {
		if _, d := DecodeResearchNode([]byte(raw), ctx); !d.Valid {
			t.Errorf("%s: an answer wrapped in prose is still an answer: %+v", name, d.Diagnostics)
		}
	}
}
