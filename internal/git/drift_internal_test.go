package git

import (
	"io"
	"strings"
	"testing"
)

func TestDriftRecordBoundStopsAfterCompleteFields(t *testing.T) {
	cancelled := 0
	out := gitBoundedOutput{limit: 1024, fieldsRemaining: 4, cancel: func() { cancelled++ }}
	for _, chunk := range []string{"header\x00pa", "th\x00next\x00second\x00uninspected\x00", "more"} {
		if n, err := out.Write([]byte(chunk)); err != nil || n != len(chunk) {
			t.Fatalf("write returned %d, %v", n, err)
		}
	}
	if out.String() != "header\x00path\x00next\x00second\x00" || cancelled != 1 || !out.recordsReached {
		t.Fatalf("record bound failed: %q, cancellation count %d", out.String(), cancelled)
	}
}

func TestDriftOutputBoundsCannotBeBypassedByCopy(t *testing.T) {
	for _, fields := range []int{0, 1} {
		cancelled := 0
		out := gitBoundedOutput{limit: 3, fieldsRemaining: fields, cancel: func() { cancelled++ }}
		// Hide WriterTo to exercise io.Copy's destination fast path as exec does.
		source := struct{ io.Reader }{strings.NewReader("abc\x00uninspected")}
		if _, err := io.Copy(&out, source); err != nil {
			t.Fatal(err)
		}
		if out.Len() != 3 || !out.exceeded || cancelled != 1 {
			t.Fatalf("copy bypassed bound: %q exceeded=%v", out.String(), out.exceeded)
		}
	}
}
