package messaging

import (
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/presentation"
)

func TestOwnerActionCharacterizationCurrentAuthorityBeforeEffect(t *testing.T) {
	for _, tc := range []struct {
		name, owner string
		channels    []string
		started     bool
	}{
		{name: "changed owner", owner: "other", channels: []string{"2"}},
		{name: "removed channel", owner: "1", channels: []string{"other"}},
		{name: "changed owner after start", owner: "other", channels: []string{"2"}, started: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := naturalExecutionDB(t)
			at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
			r := naturalAuthorizedFixture(t, db, at)
			if tc.started {
				if err := db.StartNaturalExecution(t.Context(), r.MessageID, at); err != nil {
					t.Fatal(err)
				}
			}
			s := New(db, "discord", tc.owner, tc.channels, nil, nil)
			exists, err := s.prepareNaturalExecution(t.Context(), &r, at)
			want := string(presentation.Refusal)
			state := "authorized"
			if tc.started {
				want = string(presentation.Uncertain)
				state = "executing"
			}
			if err != nil || !exists || r.OutcomeStatus != want {
				t.Fatalf("authority fence: %+v %v %v", r, exists, err)
			}
			e, err := db.ReadNaturalExecution(t.Context(), r.MessageID)
			if err != nil || e.State != state {
				t.Fatalf("refusal changed admission: %+v %v", e, err)
			}
		})
	}
}

func TestOwnerActionCharacterizationUncertainOutcomeSurvivesRestart(t *testing.T) {
	db := naturalExecutionDB(t)
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	r := naturalAuthorizedFixture(t, db, at)
	if err := db.StartNaturalExecution(t.Context(), r.MessageID, at); err != nil {
		t.Fatal(err)
	}
	const outcome = "External receipt is unresolved; inspect issue #3 before requesting again."
	if err := db.FinishNaturalExecution(t.Context(), r.MessageID, outcome, true, at); err != nil {
		t.Fatal(err)
	}
	// A fresh service reads the saved outcome without reacquiring execution.
	s := New(db, "discord", "1", []string{"2"}, nil, nil)
	exists, err := s.prepareNaturalExecution(t.Context(), &r, at.Add(time.Second))
	if err != nil || !exists || r.ReplyText != outcome || r.OutcomeStatus != string(presentation.Uncertain) {
		t.Fatalf("recovered uncertain outcome: %+v %v %v", r, exists, err)
	}
	e, err := db.ReadNaturalExecution(t.Context(), r.MessageID)
	if err != nil || e.State != "uncertain" || e.Payload != "{}" || e.Outcome != outcome {
		t.Fatalf("recovery changed receipt: %+v %v", e, err)
	}
}
