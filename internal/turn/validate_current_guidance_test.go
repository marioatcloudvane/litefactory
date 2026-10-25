package turn

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestValidateStaleChoicesKeepRefusalAndCurrentObservedDestination(t *testing.T) {
	o := newOwnerFixture(t)
	o.opts.ResearchReporter.validateHead = func(context.Context, store.Session) (string, error) { return o.head(), nil }
	o.mixed()
	before := o.state()
	session, err := o.db.GetSession(t.Context(), o.instance.SessionID)
	mustProgress(t, err)
	old, err := o.db.LatestOwnerDecisionDisplay(t.Context(), session.TaskRef, ValidateCheckCommand)
	mustProgress(t, err)
	o.say(o.command(ValidateCheckCommand, "story-002:AC-002 pass -- checked the screen"))
	current := o.state()
	if current.Revision == before.Revision {
		t.Fatal("owner check must replace current revision")
	}
	report, err := o.db.GetValidateReport(t.Context(), o.instance.ID)
	mustProgress(t, err)
	var destination string
	for _, c := range o.forge.comments {
		if c.ID == report.CommentID {
			destination = c.HTMLURL
		}
	}
	if destination == "" {
		t.Fatal("fixture needs observed report URL")
	}
	for _, body := range []string{
		ValidateCheckCommand + " " + before.Revision + " story-002:AC-002 pass -- old evidence",
		ValidateCheckCommand + " " + old.Reference + " story-002:AC-002 fail -- old evidence",
		ValidateFindingCommand + " story-002:AC-001 repair",
	} {
		o.say(body)
		notice := o.notice()
		if !strings.Contains(notice, destination) || !strings.Contains(notice, current.Revision) {
			t.Errorf("refusal lost current destination: %s", notice)
		}
		if o.state().Revision != current.Revision {
			t.Fatal("refused input changed current state")
		}
	}
	if len(o.forge.pushes)+len(o.forge.merges)+len(o.forge.issueCloses) != 0 {
		t.Fatal("refusal authorized remote effect")
	}
	// A changed head at the same revision cannot reuse the old displayed target.
	o.opts.ResearchReporter.validateHead = func(context.Context, store.Session) (string, error) { return strings.Repeat("9", 40), nil }
	guidance := validateCurrentGuidance(t.Context(), o.opts, ActivityRequest{InstanceID: o.instance.ID}, current)
	if strings.Contains(guidance, destination) || !strings.Contains(guidance, ValidateStartCommand) {
		t.Fatal(guidance)
	}
}
