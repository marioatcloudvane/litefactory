package store

import (
	"strings"
	"testing"
)

func TestRecoveryNoticeBeforeFirstBodyKeepsOriginalIdentity(t *testing.T) {
	db := newTestDB(t)
	instance := graphTestInstance(t, db)
	source := strings.Repeat("a", 64)
	first, err := db.RetainRecoveryNotice(t.Context(), instance.SessionID, source, "waiting")
	if err != nil {
		t.Fatal(err)
	}
	// The renderer failed, then retained work progressed before the next poll.
	current, err := db.RetainRecoveryNotice(t.Context(), instance.SessionID, source, "claimed")
	if err != nil {
		t.Fatal(err)
	}
	if first.Key != source || current.Key != source || current.PreviousKey != "" {
		t.Fatalf("unmaterialized source became a successor: %+v %+v", first, current)
	}
	var count int
	if err = db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM recovery_notice_dispositions`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("orphan notice metadata: %d %v", count, err)
	}
}
