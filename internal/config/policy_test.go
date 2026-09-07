package config

import "testing"

// TestContainsForbiddenFlag exercises the newly exported string-in/bool-out
// contract directly (US-14 T-40): internal/team calls this function rather
// than duplicating forbiddenFlag() or the strings.Contains check itself, so
// its behavior as a public API surface needs its own direct coverage,
// independent of whatever validateNoForbiddenFlag's existing tests already
// exercise through a *Claude config value.
//
// The flag itself is assembled from fragments, matching forbiddenFlag's own
// convention (see policy.go and purity_test.go's
// TestForbiddenFlagIsAbsentFromTheTree): writing the literal string here
// would itself trip that whole-tree scan.
func TestContainsForbiddenFlag(t *testing.T) {
	flag := "--dangerously-" + "skip-permissions"

	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{
			name:  "exact flag/reports true",
			value: flag,
			want:  true,
		},
		{
			name:  "flag embedded inside a longer string/reports true",
			value: "claude " + flag + " --model sonnet",
			want:  true,
		},
		{
			name:  "unrelated value/reports false",
			value: "--model sonnet",
			want:  false,
		},
		{
			name:  "empty value/reports false",
			value: "",
			want:  false,
		},
		{
			name:  "partial prefix only/reports false",
			value: "--dangerously-skip",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ContainsForbiddenFlag(tt.value)

			if got != tt.want {
				t.Errorf("ContainsForbiddenFlag(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
