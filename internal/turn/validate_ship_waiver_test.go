package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestOwnerShipDisplayedDeliveryCommitKeepsTestedEvidence(t *testing.T) {
	o := newOwnerFixture(t)
	o.mixed()
	before := o.state()
	tested := before.Commit
	o.write("app/displayed.go", "package app\n")
	if _, err := o.wt.Commit(t.Context(), gitCommitOptions()); err != nil {
		t.Fatal(err)
	}
	delivery := o.head()
	if _, err := o.b.RefreshValidateReport(t.Context(), o.opts, ActivityRequest{InstanceID: o.instance.ID}); err != nil {
		t.Fatal(err)
	}
	session, err := o.db.GetSession(t.Context(), o.instance.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	d, err := o.db.LatestOwnerDecisionDisplay(t.Context(), session.TaskRef, ValidateShipCommand)
	if err != nil {
		t.Fatal(err)
	}
	if d.TestedCommit != tested || d.DeliveryCommit != delivery {
		t.Fatalf("display %+v", d)
	}
	o.say(ValidateShipCommand + " " + d.Reference + " -- accept displayed drift and blockers")
	st := o.state()
	if o.final() != "delivered" || st.Commit != tested || st.Waiver == nil || st.Waiver.DeliveryCommit != delivery {
		t.Fatalf("state %+v; notice %s", st, o.notice())
	}
	if len(o.forge.pushes) != 1 || o.forge.pushes[0] != delivery || !strings.Contains(strings.Join(o.forge.merges, ","), delivery) {
		t.Fatalf("wrong delivery: %v %v", o.forge.pushes, o.forge.merges)
	}
	raw, digest := before.shippingEvidence()
	after, afterDigest := st.shippingEvidence()
	if string(raw) != string(after) || digest != afterDigest {
		t.Fatal("shipping rewrote tested evidence")
	}
}

func TestShippingWaiverFreshnessExcludesOwnDeliveryEffects(t *testing.T) {
	st := ValidateState{InstanceID: 1, Commit: strings.Repeat("a", 40), DeliveryCommit: strings.Repeat("b", 40), Assessed: true, Suite: validateSuiteOutput{Outcome: store.SuiteFailed}, Effects: ValidateDeliveryRows{Issues: map[string]store.ValidateEffect{}}, Stories: []ValidateJoinedStory{{Story: "story-001"}}}
	_, digest := st.shippingEvidence()
	st.Waiver = &store.ValidateShipWaiver{TestedCommit: st.Commit, DeliveryCommit: st.DeliveryCommit, EvidenceRevision: digest, CommentID: 9}
	st.Effects.Push = &store.ValidateEffect{Kind: store.EffectPush, State: store.EffectSucceeded, ConfirmCommentID: 9}
	if !st.waiverCurrent() {
		t.Fatal("own delivery invalidated waiver")
	}
	st.Suite.Reason = "new failed evidence"
	if st.waiverCurrent() {
		t.Fatal("changed evidence retained waiver")
	}
	st.Suite.Reason = ""
	st.Stories = append(st.Stories, ValidateJoinedStory{Story: "story-002"})
	if st.waiverCurrent() {
		t.Fatal("new blocker retained waiver")
	}
	st.Stories = st.Stories[:1]
	st.DeliveryCommit = strings.Repeat("c", 40)
	if st.waiverCurrent() {
		t.Fatal("undisplayed work retained waiver")
	}
}

func TestOwnerShipRefusesLaterUndisplayedCommitAfterDriftDisplay(t *testing.T) {
	o := newOwnerFixture(t)
	o.mixed()
	o.write("app/displayed.go", "package app\n")
	if _, err := o.wt.Commit(t.Context(), gitCommitOptions()); err != nil {
		t.Fatal(err)
	}
	if _, err := o.b.RefreshValidateReport(t.Context(), o.opts, ActivityRequest{InstanceID: o.instance.ID}); err != nil {
		t.Fatal(err)
	}
	session, err := o.db.GetSession(t.Context(), o.instance.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	display, err := o.db.LatestOwnerDecisionDisplay(t.Context(), session.TaskRef, ValidateShipCommand)
	if err != nil {
		t.Fatal(err)
	}
	o.reply(o.owner, ValidateShipCommand+" "+display.Reference+" -- ship only displayed D")
	o.write("app/undisplayed.go", "package app\n")
	if _, err := o.wt.Commit(t.Context(), gitCommitOptions()); err != nil {
		t.Fatal(err)
	}
	o.toOwner()
	if len(o.forge.pushes) != 0 || o.state().Waiver != nil || !strings.Contains(o.notice(), "branch head is now") {
		t.Fatalf("undisplayed E acted: %s", o.notice())
	}
}

func TestShippingOldTestedHeadEffectsDoNotDeliverDisplayedHead(t *testing.T) {
	o := newOwnerFixture(t)
	o.readyToPush()
	o.say(o.command(ValidatePushCommand))
	tested := o.state().Commit
	if !effectDone(o.state().Effects.Push) {
		t.Fatal("fixture did not push H")
	}
	o.write("app/delivery.go", "package app\n")
	if _, err := o.wt.Commit(t.Context(), gitCommitOptions()); err != nil {
		t.Fatal(err)
	}
	delivery := o.head()
	if _, err := o.b.RefreshValidateReport(t.Context(), o.opts, ActivityRequest{InstanceID: o.instance.ID}); err != nil {
		t.Fatal(err)
	}
	session, err := o.db.GetSession(t.Context(), o.instance.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	display, err := o.db.LatestOwnerDecisionDisplay(t.Context(), session.TaskRef, ValidateShipCommand)
	if err != nil {
		t.Fatal(err)
	}
	o.reply(o.owner, ValidateShipCommand+" "+display.Reference+" -- accept displayed D")
	o.until(func(store.GraphOwnerWait, bool) bool {
		instance, e := o.db.GetGraphInstance(t.Context(), o.instance.ID)
		if e != nil {
			t.Fatal(e)
		}
		return instance.CurrentNode == "owner_push"
	})
	st := o.state()
	if st.Effects.Push == nil || st.Effects.Push.Commit != delivery || effectDone(st.Effects.Push) || st.Effects.PR != nil {
		t.Fatalf("reused H delivery for D: %+v", st.Effects)
	}
	if st.closeEffectTarget([]int{497, 520}) == validateNumbers([]int{497, 520}) || st.Commit != tested {
		t.Fatal("delivery identity lost distinct target")
	}
}

func TestShippingChangedDeliveryEvidenceReplacesDisplayedRevision(t *testing.T) {
	st := ValidateState{InstanceID: 1, Commit: strings.Repeat("a", 40), Effects: ValidateDeliveryRows{Merge: &store.ValidateEffect{ID: 5, Kind: store.EffectMerge, State: store.EffectFailed, Error: "first failure", ConfirmCommentID: 10}}}
	before := st.revision()
	st.Effects.Merge.Error = "different failure evidence"
	if st.revision() == before {
		t.Fatal("changed delivery evidence retained displayed decision revision")
	}
}
