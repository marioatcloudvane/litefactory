package github

import "testing"

// The Link-header parser is the whole reason this package has no SDK
// dependency, so it is tested directly rather than only through the endpoints.
// Everything below is a shape GitHub or an intermediary actually emits.

func TestNextLink(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header string
		want   string
	}{
		{
			name:   "absent",
			header: "",
		},
		{
			name:   "next alone",
			header: `<https://api.github.com/repositories/1/issues?page=2>; rel="next"`,
			want:   "https://api.github.com/repositories/1/issues?page=2",
		},
		{
			name: "next then last, GitHub's usual order",
			header: `<https://api.github.com/x?page=2>; rel="next", ` +
				`<https://api.github.com/x?page=9>; rel="last"`,
			want: "https://api.github.com/x?page=2",
		},
		{
			name: "next after prev and first",
			header: `<https://api.github.com/x?page=1>; rel="prev", ` +
				`<https://api.github.com/x?page=1>; rel="first", ` +
				`<https://api.github.com/x?page=3>; rel="next"`,
			want: "https://api.github.com/x?page=3",
		},
		{
			name:   "the last page has no next",
			header: `<https://api.github.com/x?page=1>; rel="prev", <https://api.github.com/x?page=1>; rel="first"`,
		},
		{
			name:   "an unquoted relation",
			header: `<https://api.github.com/x?page=2>; rel=next`,
			want:   "https://api.github.com/x?page=2",
		},
		{
			name:   "several relations in one quoted value",
			header: `<https://api.github.com/x?page=2>; rel="prev next"`,
			want:   "https://api.github.com/x?page=2",
		},
		{
			name:   "a relation with unusual casing",
			header: `<https://api.github.com/x?page=2>; REL="NEXT"`,
			want:   "https://api.github.com/x?page=2",
		},
		{
			name:   "other parameters before the relation",
			header: `<https://api.github.com/x?page=2>; type="application/json"; rel="next"`,
			want:   "https://api.github.com/x?page=2",
		},
		{
			// GitHub's cursor pagination puts commas inside the query string,
			// so splitting link-values on a comma loses the target.
			name:   "a comma inside the target",
			header: `<https://api.github.com/x?after=a,b,c&page=2>; rel="next"`,
			want:   "https://api.github.com/x?after=a,b,c&page=2",
		},
		{
			name:   "surrounding whitespace",
			header: `  < https://api.github.com/x?page=2 > ;  rel = "next" `,
			want:   "https://api.github.com/x?page=2",
		},
		{
			name:   "a malformed value with no closing bracket",
			header: `<https://api.github.com/x?page=2; rel="next"`,
		},
		{
			name:   "a header with no link-value at all",
			header: `rel="next"`,
		},
		{
			name:   "only relations this package does not follow",
			header: `<https://api.github.com/x?page=9>; rel="last"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := nextLink(tc.header); got != tc.want {
				t.Errorf("nextLink(%q) = %q, want %q", tc.header, got, tc.want)
			}
		})
	}
}

// TestNextLinkTerminates guards the parser's loop. cutLinkValue must consume
// input on every iteration, or a header that never yields a rel="next" hangs
// the tick rather than failing it.
func TestNextLinkTerminates(t *testing.T) {
	t.Parallel()

	done := make(chan string, 1)
	go func() {
		done <- nextLink(`<a>; rel="x", <b>; rel="y", <<<>>>, <c>`)
	}()

	select {
	case got := <-done:
		if got != "" {
			t.Errorf("nextLink = %q, want empty", got)
		}
	case <-t.Context().Done():
		t.Fatal("nextLink did not terminate")
	}
}
