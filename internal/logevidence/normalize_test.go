package logevidence

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeRecordDecodesEveryField(t *testing.T) {
	raw := []byte(`{"type":"assistant","metadata":{"sec\u0072et":"known-\u0073ecret"},"message":{"content":[{"type":"tool_use","input":{"password":"known-\u0073ecret"}}]}}`)
	text, kind, err := NormalizeRecord(raw)
	if err != nil || kind != "assistant" || strings.Count(text, "known-secret") != 2 || !strings.Contains(text, "secret:") {
		t.Fatalf("unexpected normalization: %q %q %v", text, kind, err)
	}
}

func TestNormalizeRecordWithholdsUnsafeFormats(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want error
	}{
		{`{"type":"assistant","type":"user"}`, ErrMalformedRecord},
		{`{"type":"assistant"`, ErrMalformedRecord},
		{`{"type":"future","text":"secret"}`, ErrUnsupportedRecord},
		{`{"type":"assistant","message":{"content":[{"type":"image","data":"secret"}]}}`, ErrUnsupportedRecord},
		{`{"type":"item.completed","item":{"type":"future","text":"secret"}}`, ErrUnsupportedRecord},
	} {
		text, _, err := NormalizeRecord([]byte(test.raw))
		if text != "" || !errors.Is(err, test.want) {
			t.Errorf("unsafe record produced %q, %v", text, err)
		}
	}
}
