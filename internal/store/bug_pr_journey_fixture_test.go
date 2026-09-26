package store

import "testing"

// BugPRJourneyFixture supplies retained upstream observations, not real fixer or
// check execution. Delivery activities and SQLite persistence run for real.
func BugPRJourneyFixture(t *testing.T, db *DB) BugTestCall {
	t.Helper()
	r := testChildRequest(t, db)
	c := testChildCreate(t, db, r)
	for _, phase := range []string{"regression", "focused", "full"} {
		testChildFinishPhase(t, db, c, phase, "passed")
	}
	if _, err := db.SettleBugTestCall(t.Context(), c.ID, graphTestTime()); err != nil {
		t.Fatal(err)
	}
	if err := testChildConsume(t, db, c, "passed"); err != nil {
		t.Fatal(err)
	}
	c, err := db.GetBugTestCall(t.Context(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func BugPRJourneyRepairCount(t *testing.T, db *DB, session int64) int {
	t.Helper()
	var count int
	if err := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM bug_repairs WHERE session_id=?`, session).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
