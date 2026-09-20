package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestClaimGraphOwnerReply_RejectsInvalidAdmissionBeforeStorage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(*GraphOwnerReply)
	}{
		{"missing owner", func(in *GraphOwnerReply) { in.OwnerID = 0 }},
		{"non-owner author", func(in *GraphOwnerReply) { in.AuthorID = 8 }},
		{"missing reply identity", func(in *GraphOwnerReply) { in.CommentID = 0 }},
		{"missing question identity", func(in *GraphOwnerReply) { in.QuestionCommentID = 0 }},
		{"missing mode", func(in *GraphOwnerReply) { in.Mode = "" }},
		{"missing task", func(in *GraphOwnerReply) { in.TaskRef = "" }},
		{"unrelated source", func(in *GraphOwnerReply) { in.SourceRef = "github:example/repo#342" }},
		{"missing publication correlation", func(in *GraphOwnerReply) { in.DeliveryKey = "" }},
		{"empty reply", func(in *GraphOwnerReply) { in.Body = "" }},
		{"whitespace reply", func(in *GraphOwnerReply) { in.Body = " \t\n" }},
		{"oversized reply", func(in *GraphOwnerReply) { in.Body = strings.Repeat("a", 65537) }},
		{"byte limit rather than rune count", func(in *GraphOwnerReply) { in.Body = strings.Repeat("é", 32769) }},
		{"missing event time", func(in *GraphOwnerReply) { in.At = time.Time{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := GraphOwnerReply{InstanceID: 1, OwnerID: 7, AuthorID: 7, CommentID: 12, QuestionCommentID: 11, TaskRef: "github:example/repo#341", SourceRef: "github:example/repo#341", Mode: "research", DeliveryKey: "question-1", Body: "Continue with the saved findings.", At: time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)}
			tc.mutate(&in)
			// Invalid external input must be rejected without consulting storage.
			var db *DB
			got, err := db.ClaimGraphOwnerReply(t.Context(), in)
			if !errors.Is(err, ErrGraphInvariant) {
				t.Fatalf("error=%v, want ErrGraphInvariant", err)
			}
			if got != (GraphOwnerWait{}) {
				t.Fatalf("invalid admission returned a claim: %+v", got)
			}
		})
	}
}
