package commandtext

import "testing"

func TestNormalize(t *testing.T) {
	for _, tt := range []struct{ body, want string }{
		{" \r\n\t`/plan work` \r\nnext `line`", " \r\n\t/plan work \r\nnext `line`"},
		{"`/approve-bug analysis 1`\nnotes", "/approve-bug analysis 1\nnotes"},
		{"`/plan`", "/plan"},
	} {
		if got := Normalize(tt.body); got != tt.want {
			t.Errorf("%q => %q, want %q", tt.body, got, tt.want)
		}
	}
	for _, body := range []string{"", " ", "`/`", "`/usr/bin`", "`/-plan`", "` /plan`", "``/plan``", "```\n/plan\n```", "`/plan `work`", "`/plan", "`/plan` prose", "> `/plan`", "prose\n`/plan`", "`ordinary prose`", "/plan `work`"} {
		if got := Normalize(body); got != body {
			t.Errorf("changed non-span %q to %q", body, got)
		}
	}
}
