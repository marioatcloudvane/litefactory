package logreader

import (
	"bytes"
	"strings"
	"testing"
)

func TestAssemblePreservesPrefixAndChargesLookahead(t *testing.T) {
	text := strings.Repeat("ä🙂", 900) + "\nnext\n"
	source := strings.NewReader(text)
	var destination bytes.Buffer
	offset := int64(0)
	for {
		page, err := Assemble(t.Context(), source, &destination, offset, 137)
		if err != nil {
			t.Fatal(err)
		}
		if page.Scanned > 137 || page.Offset <= offset {
			t.Fatalf("unbounded or missing progress: %+v", page)
		}
		offset = page.Offset
		if page.Complete {
			break
		}
	}
	if got, want := destination.String(), strings.TrimSuffix(text, "next\n"); got != want {
		t.Fatalf("record changed: got %d bytes want %d", len(got), len(want))
	}
	if offset != int64(destination.Len()) {
		t.Fatal("lookahead advanced past unfinished bytes")
	}
}
