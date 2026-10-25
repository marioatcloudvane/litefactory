package store

import "testing"

func TestOwnerReplyParentIsImmutableAndDistinctFromWaitQuestion(t *testing.T) {
	db, reply := predatingReplyFixture(t, true)
	instance, err := db.GetGraphInstance(t.Context(), reply.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	reply.ReplyToCommentID = 55
	source := Comment{SessionID: instance.SessionID, ForgeCommentID: reply.CommentID, AuthorUserID: reply.AuthorID, Body: reply.Body, ReplyToCommentID: reply.ReplyToCommentID, Classification: ClassificationInstruction}
	saved, err := db.RecordComment(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ReplyToCommentID != 55 {
		t.Fatal("source lost explicit parent")
	}
	source.ReplyToCommentID = 56
	if _, err = db.RecordComment(t.Context(), source); err == nil {
		t.Fatal("source replay changed explicit parent")
	}
	wait, err := db.ClaimGraphOwnerReply(t.Context(), reply)
	if err != nil {
		t.Fatal(err)
	}
	if wait.ReplyToCommentID != 55 || wait.ReplyToCommentID == reply.QuestionCommentID {
		t.Fatalf("active question confused with source parent: %+v", wait)
	}
	read, err := db.GetGraphOwnerWait(t.Context(), reply.InstanceID)
	if err != nil || read.ReplyToCommentID != 55 {
		t.Fatalf("retained parent: %+v %v", read, err)
	}
}
