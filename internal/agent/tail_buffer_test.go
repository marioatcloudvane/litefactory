package agent_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestTailBuffer_RetainsFinalBytes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limit  int
		writes []string
		want   string
	}{
		{"empty", 5, nil, ""},
		{"exact limit", 5, []string{"ab", "cde", ""}, "abcde"},
		{"multiple writes", 5, []string{"abc", "def", "gh"}, "[earlier stderr dropped]\ndefgh"},
		{"large write then append", 5, []string{strings.Repeat("x", 1<<20) + "12345", "67"}, "[earlier stderr dropped]\n34567"},
		{"zero limit", 0, []string{"discard"}, "[earlier stderr dropped]\n"},
		{"negative limit", -1, []string{"discard"}, "[earlier stderr dropped]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := agent.NewTailBuffer(tc.limit)
			for _, input := range tc.writes {
				if n, err := b.Write([]byte(input)); err != nil || n != len(input) {
					t.Fatalf("write=(%d,%v), want %d", n, err, len(input))
				}
			}
			if got := b.String(); got != tc.want {
				t.Fatalf("tail=%q want %q", got, tc.want)
			}
		})
	}
}
