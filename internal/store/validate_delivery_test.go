package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

var deliveryCommit = strings.Repeat("c", 40)

func TestValidateReportKeepsItsCommentID(t *testing.T) {
	db := newTestDB(t)
	at := graphTestTime()
	if _, err := db.GetValidateReport(t.Context(), 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("before: %v", err)
	}
	if err := db.PutValidateReport(t.Context(), ValidateReport{InstanceID: 1, UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	if err := db.PutValidateReport(t.Context(), ValidateReport{InstanceID: 1, CommentID: 7, Marker: "m", Revision: "r1", Digest: "d1", UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	if err := db.PutValidateReport(t.Context(), ValidateReport{InstanceID: 1, CommentID: 8, UpdatedAt: at}); err == nil {
		t.Fatal("comment id was replaced")
	}
	if err := db.PutValidateReport(t.Context(), ValidateReport{InstanceID: 1, CommentID: 0, UpdatedAt: at}); err == nil {
		t.Fatal("comment id was erased")
	}
	if err := db.PutValidateReport(t.Context(), ValidateReport{InstanceID: 1, CommentID: 7, Marker: "m", Revision: "r2", Digest: "d2", Notice: "refused: stale", UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetValidateReport(t.Context(), 1)
	if err != nil || got.CommentID != 7 || got.Revision != "r2" || got.Notice != "refused: stale" || got.Marker != "m" {
		t.Fatalf("stored %+v %v", got, err)
	}
	if err := db.PutValidateReport(t.Context(), ValidateReport{InstanceID: 0, UpdatedAt: at}); err == nil {
		t.Fatal("invalid report accepted")
	}
}

func deliveryVerdict(comment int64, verdict string) ValidateVerdict {
	return ValidateVerdict{InstanceID: 1, Commit: deliveryCommit, Criterion: "AC-1", Verdict: verdict, Evidence: " looked ", CommentID: comment, AuthorID: 5, CreatedAt: graphTestTime()}
}

func TestValidateVerdictReplacementKeepsHistory(t *testing.T) {
	db := newTestDB(t)
	first, err := db.PutValidateVerdict(t.Context(), deliveryVerdict(10, VerdictFail))
	if err != nil || !first.Current || first.ID < 1 || first.Evidence != "looked" {
		t.Fatalf("first %+v %v", first, err)
	}
	second, err := db.PutValidateVerdict(t.Context(), deliveryVerdict(11, VerdictPass))
	if err != nil || second.ID == first.ID {
		t.Fatalf("second %+v %v", second, err)
	}
	replay, err := db.PutValidateVerdict(t.Context(), deliveryVerdict(10, VerdictPass))
	if err != nil || replay.ID != first.ID || replay.Verdict != VerdictFail || replay.Current {
		t.Fatalf("replay %+v %v", replay, err)
	}
	cur, err := db.ListValidateVerdicts(t.Context(), 1, deliveryCommit, false)
	if err != nil || len(cur) != 1 || cur[0].ID != second.ID || cur[0].Verdict != VerdictPass {
		t.Fatalf("current %+v %v", cur, err)
	}
	all, err := db.ListValidateVerdicts(t.Context(), 1, deliveryCommit, true)
	if err != nil || len(all) != 2 || all[0].ID != first.ID || all[0].Current || !all[1].Current {
		t.Fatalf("history %+v %v", all, err)
	}
}

func TestValidateVerdictRefusesInvalidInput(t *testing.T) {
	db := newTestDB(t)
	for name, mod := range map[string]func(*ValidateVerdict){
		"empty evidence": func(v *ValidateVerdict) { v.Evidence = "  " },
		"short commit":   func(v *ValidateVerdict) { v.Commit = "abc" },
		"bad verdict":    func(v *ValidateVerdict) { v.Verdict = "maybe" },
		"no criterion":   func(v *ValidateVerdict) { v.Criterion = "" },
		"no comment":     func(v *ValidateVerdict) { v.CommentID = 0 },
		"no author":      func(v *ValidateVerdict) { v.AuthorID = 0 },
		"no instance":    func(v *ValidateVerdict) { v.InstanceID = 0 },
		"zero created":   func(v *ValidateVerdict) { v.CreatedAt = time.Time{} },
	} {
		v := deliveryVerdict(1, VerdictPass)
		mod(&v)
		if _, err := db.PutValidateVerdict(t.Context(), v); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestValidateVerdictAndChoiceCannotBeEditedInPlace(t *testing.T) {
	db := newTestDB(t)
	v, err := db.PutValidateVerdict(t.Context(), deliveryVerdict(10, VerdictPass))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`UPDATE validate_verdicts SET verdict='fail' WHERE id=?`,
		`UPDATE validate_verdicts SET evidence='other' WHERE id=?`,
	} {
		if _, err := db.sql.ExecContext(t.Context(), q, v.ID); err == nil {
			t.Errorf("%s accepted", q)
		}
	}
	c, err := db.PutValidateChoice(t.Context(), ValidateChoice{InstanceID: 1, Commit: deliveryCommit, Finding: "F1", Choice: ChoiceIssue, IssueKind: "bug", CommentID: 20, AuthorID: 5, CreatedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`UPDATE validate_choices SET choice='defer', issue_kind='' WHERE id=?`,
		`UPDATE validate_choices SET issue_kind='story' WHERE id=?`,
	} {
		if _, err := db.sql.ExecContext(t.Context(), q, c.ID); err == nil {
			t.Errorf("%s accepted", q)
		}
	}
	// A replaced row cannot be revived.
	if _, err := db.PutValidateVerdict(t.Context(), deliveryVerdict(11, VerdictFail)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE validate_verdicts SET current=1 WHERE id=?`, v.ID); err == nil {
		t.Error("replaced verdict revived")
	}
}

func TestValidateChoiceReplacementAndIssueKind(t *testing.T) {
	db := newTestDB(t)
	base := ValidateChoice{InstanceID: 1, Commit: deliveryCommit, Finding: "F1", CommentID: 20, AuthorID: 5, CreatedAt: graphTestTime()}
	for name, mod := range map[string]func(*ValidateChoice){
		"issue without kind": func(c *ValidateChoice) { c.Choice = ChoiceIssue },
		"repair with kind":   func(c *ValidateChoice) { c.Choice = ChoiceRepair; c.IssueKind = "bug" },
		"defer with kind":    func(c *ValidateChoice) { c.Choice = ChoiceDefer; c.IssueKind = "story" },
		"unknown kind":       func(c *ValidateChoice) { c.Choice = ChoiceIssue; c.IssueKind = "epic" },
		"unknown choice":     func(c *ValidateChoice) { c.Choice = "ignore" },
		"no finding":         func(c *ValidateChoice) { c.Choice = ChoiceRepair; c.Finding = "" },
	} {
		c := base
		mod(&c)
		if _, err := db.PutValidateChoice(t.Context(), c); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	a := base
	a.Choice = ChoiceRepair
	first, err := db.PutValidateChoice(t.Context(), a)
	if err != nil || !first.Current {
		t.Fatalf("first %+v %v", first, err)
	}
	b := base
	b.Choice, b.IssueKind, b.CommentID = ChoiceIssue, "story", 21
	second, err := db.PutValidateChoice(t.Context(), b)
	if err != nil || second.ID == first.ID || second.IssueKind != "story" {
		t.Fatalf("second %+v %v", second, err)
	}
	replay, err := db.PutValidateChoice(t.Context(), a)
	if err != nil || replay.ID != first.ID {
		t.Fatalf("replay %+v %v", replay, err)
	}
	cur, err := db.ListValidateChoices(t.Context(), 1, deliveryCommit, false)
	if err != nil || len(cur) != 1 || cur[0].ID != second.ID {
		t.Fatalf("current %+v %v", cur, err)
	}
	all, err := db.ListValidateChoices(t.Context(), 1, deliveryCommit, true)
	if err != nil || len(all) != 2 || all[0].Current || !all[1].Current {
		t.Fatalf("history %+v %v", all, err)
	}
}

func deliveryEffect(confirm int64) ValidateEffect {
	return ValidateEffect{InstanceID: 1, Kind: EffectPush, Commit: deliveryCommit, Target: "main", ConfirmCommentID: confirm, CreatedAt: graphTestTime()}
}

func TestBeginValidateEffectCreatesIntentOnce(t *testing.T) {
	db := newTestDB(t)
	first, created, err := db.BeginValidateEffect(t.Context(), deliveryEffect(3))
	if err != nil || !created || first.State != EffectIntent || first.ID < 1 {
		t.Fatalf("first %+v %v %v", first, created, err)
	}
	again, created, err := db.BeginValidateEffect(t.Context(), deliveryEffect(3))
	if err != nil || created || again.ID != first.ID || again.State != EffectIntent {
		t.Fatalf("again %+v %v %v", again, created, err)
	}
	newer, created, err := db.BeginValidateEffect(t.Context(), deliveryEffect(4))
	if err != nil || created || newer.ID != first.ID || newer.ConfirmCommentID != 3 {
		t.Fatalf("intent with newer comment %+v %v %v", newer, created, err)
	}
	bad := deliveryEffect(3)
	bad.Kind = "deploy"
	if _, _, err := db.BeginValidateEffect(t.Context(), bad); err == nil {
		t.Fatal("unknown kind accepted")
	}
	// Same target, other kind is a separate effect.
	other := deliveryEffect(3)
	other.Kind = EffectPR
	o, created, err := db.BeginValidateEffect(t.Context(), other)
	if err != nil || !created || o.ID == first.ID {
		t.Fatalf("other kind %+v %v %v", o, created, err)
	}
	// Another run has its own.
	run2 := deliveryEffect(3)
	run2.InstanceID = 2
	if r, created, err := db.BeginValidateEffect(t.Context(), run2); err != nil || !created || r.ID == first.ID {
		t.Fatalf("other run %+v %v %v", r, created, err)
	}
	// Unique key is enforced by the table.
	if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO validate_effects(instance_id,kind,commit_sha,target,state,created_at,updated_at) VALUES(1,'push',?,'main','intent','x','x')`, deliveryCommit); err == nil {
		t.Fatal("duplicate effect key accepted")
	}
	list, err := db.ListValidateEffects(t.Context(), 1)
	if err != nil || len(list) != 2 {
		t.Fatalf("list %+v %v", list, err)
	}
	got, err := db.GetValidateEffect(t.Context(), 1, EffectPush, "main")
	if err != nil || got.ID != first.ID {
		t.Fatalf("get %+v %v", got, err)
	}
	if _, err := db.GetValidateEffect(t.Context(), 1, EffectMerge, "main"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestFailedEffectRetriesOnlyWithNewerConfirmation(t *testing.T) {
	for _, state := range []string{EffectFailed, EffectUncertain} {
		db := newTestDB(t)
		e, _, err := db.BeginValidateEffect(t.Context(), deliveryEffect(5))
		if err != nil {
			t.Fatal(err)
		}
		if err := db.SettleValidateEffect(t.Context(), e.ID, state, nil, "boom", graphTestTime()); err != nil {
			t.Fatal(err)
		}
		for _, c := range []int64{5, 4} {
			got, created, err := db.BeginValidateEffect(t.Context(), deliveryEffect(c))
			if err != nil || created || got.State != state || got.Error != "boom" {
				t.Fatalf("%s confirm %d: %+v %v %v", state, c, got, created, err)
			}
		}
		got, created, err := db.BeginValidateEffect(t.Context(), deliveryEffect(6))
		if err != nil || !created || got.ID != e.ID || got.State != EffectIntent || got.ConfirmCommentID != 6 || got.Error != "" {
			t.Fatalf("%s retry: %+v %v %v", state, got, created, err)
		}
		stored, err := db.GetValidateEffect(t.Context(), 1, EffectPush, "main")
		if err != nil || stored.State != EffectIntent || stored.ConfirmCommentID != 6 || stored.Error != "" {
			t.Fatalf("stored %+v %v", stored, err)
		}
	}
}

func TestSucceededEffectIsImmutable(t *testing.T) {
	db := newTestDB(t)
	e, _, err := db.BeginValidateEffect(t.Context(), deliveryEffect(5))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SettleValidateEffect(t.Context(), e.ID, "intent", nil, "", graphTestTime()); err == nil {
		t.Fatal("invalid state accepted")
	}
	if err := db.SettleValidateEffect(t.Context(), 9999, EffectFailed, nil, "", graphTestTime()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing effect: %v", err)
	}
	result := json.RawMessage(`{"sha":"abc"}`)
	if err := db.SettleValidateEffect(t.Context(), e.ID, EffectSucceeded, result, "", graphTestTime()); err != nil {
		t.Fatal(err)
	}
	if err := db.SettleValidateEffect(t.Context(), e.ID, EffectSucceeded, result, "", graphTestTime()); err != nil {
		t.Fatalf("same result: %v", err)
	}
	if err := db.SettleValidateEffect(t.Context(), e.ID, EffectSucceeded, json.RawMessage(`{"sha":"def"}`), "", graphTestTime()); !errors.Is(err, ErrEffectSucceeded) {
		t.Fatalf("different result: %v", err)
	}
	if err := db.SettleValidateEffect(t.Context(), e.ID, EffectFailed, result, "x", graphTestTime()); !errors.Is(err, ErrEffectSucceeded) {
		t.Fatalf("failed after success: %v", err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE validate_effects SET error='x' WHERE id=?`, e.ID); err == nil {
		t.Fatal("direct update of a succeeded effect accepted")
	}
	got, _, err := db.BeginValidateEffect(t.Context(), deliveryEffect(99))
	if err != nil || got.State != EffectSucceeded || string(got.Result) != string(result) {
		t.Fatalf("begin after success %+v %v", got, err)
	}
}

func TestArchiveRegistryCoversValidateDeliveryTables(t *testing.T) {
	names := map[string]bool{}
	for _, e := range ArchiveRegistry() {
		names[e.Name] = true
	}
	for _, n := range []string{"validate_reports", "validate_verdicts", "validate_choices", "validate_effects"} {
		if !names[n] {
			t.Errorf("%s missing from the archive registry", n)
		}
	}
}
