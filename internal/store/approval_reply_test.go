package store

import "testing"

func TestIsApprovalReplyAcceptsOnlyTheFixedShipSet(t *testing.T) {
	for _, tc := range []struct {
		body, token string
		want        bool
	}{
		{"ship", "ship", true},
		{"SHIP", "ship", true},
		{" ship it! ", "ship", true},
		{"Ship  it.", "ship", true},
		{"ship?", "ship", true},
		{"ship it", "ship", true},
		{"ship please", "ship", false},
		{"don't ship", "ship", false},
		{"shipit", "ship", false},
		{"ship it now", "ship", false},
		{"ship it later", "ship", false},
		{"", "ship", false},
		{"!", "ship", false},
		{"ship", "", false},
		{"", "", false},
		{"done!", "done", true},
		{"done it", "done", false},
	} {
		if got := IsApprovalReply(tc.body, tc.token); got != tc.want {
			t.Errorf("IsApprovalReply(%q, %q) = %v, want %v", tc.body, tc.token, got, tc.want)
		}
	}
}
