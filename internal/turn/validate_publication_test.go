package turn

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

func validateOverflowFixture(t *testing.T) (*ownerFixture, []store.ReportDelivery) {
	t.Helper()
	o := newOwnerFixture(t)
	o.mixed()
	r := o.opts.ResearchReporter
	r.validateHead = func(context.Context, store.Session) (string, error) { return o.head(), nil }
	st := o.state()
	st.DeliveryCommit = o.head()
	st.Findings = nil
	for i := 0; i < 400; i++ {
		st.Findings = append(st.Findings, ValidateFinding{ID: fmt.Sprintf("story-002:AC-%03d", i), Story: "story-002", Name: fmt.Sprintf("AC-%03d", i), State: findingAwaitingCheck, Status: "unverified", Criterion: true})
	}
	task, err := naming.ParseRef("github:acme/api#497")
	if err != nil {
		t.Fatal(err)
	}
	text, parts, err := r.prepareValidateGuidance(t.Context(), o.instance.SessionID, task, st, o.b.Profile, testValidateLayout("Retained overview\n", st), "", o.clock)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) < 2 {
		t.Fatal("fixture must overflow")
	}
	if _, err = r.ReportValidate(t.Context(), st.InstanceID, st.Revision, "", text, o.clock); err != nil {
		t.Fatal(err)
	}
	return o, parts
}

func TestValidateGuidanceIndexRecoveryAndSupersededReceipt(t *testing.T) {
	for _, mode := range []string{"edit failure", "superseded"} {
		t.Run(mode, func(t *testing.T) {
			o, parts := validateOverflowFixture(t)
			r := o.opts.ResearchReporter
			o.forge.editErr = github.ErrRateLimited
			var failed bool
			for _, part := range parts {
				_, err := r.dispatchDelivery(t.Context(), part)
				failed = failed || err != nil
			}
			if !failed {
				t.Fatal("failed index edit must remain pending")
			}
			saved, err := o.db.GetValidateReport(t.Context(), o.instance.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(o.forge.bodies("Partial publication")[0].Body, "incomplete") {
				t.Fatal("partial index must stay explicit")
			}
			if mode == "superseded" {
				saved.Revision = "abcdef123456"
				saved.Digest = "new-report"
				if err = o.db.PutValidateReport(t.Context(), saved); err != nil {
					t.Fatal(err)
				}
			}
			o.forge.editErr = nil
			before := o.forge.edits
			if err = r.RecoverValidatePublications(t.Context(), o.instance.SessionID); err != nil {
				t.Fatal(err)
			}
			rows, err := o.db.ListSessionReportDeliveries(t.Context(), o.instance.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			var index store.ReportDelivery
			for _, d := range rows {
				if strings.HasPrefix(d.Purpose, validateIndexPurpose) {
					index = d
				}
			}
			if mode != "superseded" && !index.Completed {
				t.Fatal("index edit was not settled")
			}
			if mode == "superseded" {
				if index.Key != "" || o.forge.edits != before {
					t.Fatal("superseded edit invented a publication receipt or changed current guidance")
				}
			} else {
				if index.CommentID == 0 {
					t.Fatal("completed index lacks real parent receipt")
				}
				current, _ := o.db.GetValidateReport(t.Context(), o.instance.ID)
				if current.Digest == saved.Digest {
					t.Fatal("recovery did not complete current index")
				}
				edits := o.forge.edits
				if err = r.RecoverValidatePublications(t.Context(), o.instance.SessionID); err != nil || o.forge.edits != edits {
					t.Fatal("recovery repeated a settled edit")
				}
			}
		})
	}
}

type validateLostPartForge struct {
	*ownerForge
	lost bool
}

func (f *validateLostPartForge) PostComment(ctx context.Context, repo github.Repo, issue int, body redact.Body) (github.Comment, error) {
	c, err := f.ownerForge.PostComment(ctx, repo, issue, body)
	if err != nil {
		return c, err
	}
	f.mu.Lock()
	for i := range f.comments {
		if f.comments[i].ID == c.ID {
			f.comments[i].HTMLURL = fmt.Sprintf("https://github.com/%s/%s/issues/%d#issuecomment-%d", repo.Owner, repo.Name, issue, c.ID)
			c = f.comments[i]
		}
	}
	f.mu.Unlock()
	if !f.lost && strings.Contains(body.String(), "Validate decision guidance") {
		f.lost = true
		return github.Comment{}, errors.New("lost provider response")
	}
	return c, nil
}

func TestValidateGuidanceLostPartResponseUsesRetainedBytes(t *testing.T) {
	o, parts := validateOverflowFixture(t)
	r := o.opts.ResearchReporter
	forge := &validateLostPartForge{ownerForge: o.forge}
	r.forge = forge
	_, err := r.dispatchDelivery(t.Context(), parts[0])
	if err == nil {
		t.Fatal("lost response must remain uncertain")
	}
	first, err := o.db.GetReportDelivery(t.Context(), parts[0].Key)
	if err != nil {
		t.Fatal(err)
	}
	if !first.PossiblySent || first.CommentID != 0 {
		t.Fatal("lost receipt was incorrectly confirmed")
	}
	count := len(o.forge.comments)
	if err = r.RecoverValidatePublications(t.Context(), o.instance.SessionID); err != nil {
		t.Fatal(err)
	}
	recovered, err := o.db.GetReportDelivery(t.Context(), parts[0].Key)
	if err != nil {
		t.Fatal(err)
	}
	if !recovered.Completed || recovered.CommentID == 0 || recovered.Body != first.Body {
		t.Fatal("recovery did not reuse immutable part and observed receipt")
	}
	if len(o.forge.comments) != count+len(parts)-1 {
		t.Fatal("lost-response recovery duplicated a part")
	}
}

func TestValidateDecisionPackingKeepsWholeChoices(t *testing.T) {
	st := ValidateState{Revision: "abcdef123456", Findings: []ValidateFinding{{ID: "s:code", Story: "s", Name: "code", State: findingOpen, Blocking: true}, {ID: "s:hint", Story: "s", Name: "hint", State: findingOpen, Optional: true}}}
	items := validateDecisionItems(st)
	parts, err := packValidateDecisionItems(items, len(items[0]))
	if err != nil || len(parts) != 2 {
		t.Fatalf("pack intact choices: %d %v", len(parts), err)
	}
	if !strings.Contains(parts[0], "accept -- <reason>") || strings.Contains(parts[0], " defer`") || !strings.Contains(parts[1], " defer`") {
		t.Fatal("packing changed permitted choices")
	}
	if _, err = packValidateDecisionItems(items, 10); err == nil {
		t.Fatal("oversized intact item must fail explicitly")
	}
}

func TestValidateGuidanceMovedHeadDoesNotPublishOldIndex(t *testing.T) {
	o, parts := validateOverflowFixture(t)
	r := o.opts.ResearchReporter
	r.validateHead = func(context.Context, store.Session) (string, error) { return strings.Repeat("9", 40), nil }
	before := o.forge.edits
	var refused bool
	for _, part := range parts {
		_, err := r.dispatchDelivery(t.Context(), part)
		refused = refused || err != nil
	}
	if !refused || o.forge.edits != before {
		t.Fatal("changed head must leave partial index unchanged")
	}
	r.validateHead = func(context.Context, store.Session) (string, error) { return o.head(), nil }
	if err := r.RecoverValidatePublications(t.Context(), o.instance.SessionID); err != nil {
		t.Fatal(err)
	}
	if o.forge.edits == before {
		t.Fatal("current head recovery did not complete index")
	}
}

type validateLostIndexForge struct {
	*ownerForge
	lost bool
}

func (f *validateLostIndexForge) EditComment(ctx context.Context, repo github.Repo, id int64, body redact.Body) error {
	if err := f.ownerForge.EditComment(ctx, repo, id, body); err != nil {
		return err
	}
	if !f.lost && strings.Contains(body.String(), "All numbered parts for this revision are confirmed") {
		f.lost = true
		return errors.New("lost index edit response")
	}
	return nil
}

func TestValidateGuidanceLateOldIndexReceiptDoesNotReplaceCurrent(t *testing.T) {
	o, parts := validateOverflowFixture(t)
	r := o.opts.ResearchReporter
	r.forge = &validateLostIndexForge{ownerForge: o.forge}
	var uncertain bool
	for _, part := range parts {
		_, err := r.dispatchDelivery(t.Context(), part)
		uncertain = uncertain || err != nil
	}
	if !uncertain {
		t.Fatal("lost edit response must remain uncertain")
	}
	saved, err := o.db.GetValidateReport(t.Context(), o.instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	saved.Revision = "abcdef123456"
	saved.Digest = "current-index"
	if err = o.db.PutValidateReport(t.Context(), saved); err != nil {
		t.Fatal(err)
	}
	before := o.forge.edits
	if err = r.RecoverValidatePublications(t.Context(), o.instance.SessionID); err != nil {
		t.Fatal(err)
	}
	if o.forge.edits != before {
		t.Fatal("late old receipt reapplied old index")
	}
	current, err := o.db.GetValidateReport(t.Context(), o.instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision != saved.Revision || current.Digest != saved.Digest {
		t.Fatal("late receipt replaced current revision")
	}
	rows, err := o.db.ListSessionReportDeliveries(t.Context(), o.instance.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range rows {
		if strings.HasPrefix(d.Purpose, validateIndexPurpose) {
			found = true
			if !d.Completed || d.CommentID == 0 {
				t.Fatal("late observed provider receipt did not settle old immutable delivery")
			}
		}
	}
	if !found {
		t.Fatal("provider edit evidence was discarded")
	}
}

func TestValidateGuidanceRecoveryFinishesIndexAfterPartsCompletedBeforeOverview(t *testing.T) {
	o, parts := validateOverflowFixture(t)
	r := o.opts.ResearchReporter
	m, err := r.validateManifest(t.Context(), parts[0])
	if err != nil {
		t.Fatal(err)
	}
	saved, err := o.db.GetValidateReport(t.Context(), o.instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Restore the exact report binding that existed when the first immutable
	// manifest was retained, simulating a crash before its partial overview.
	saved.Digest = m.PreviousDigest
	saved.Revision = m.PreviousRevision
	if err = o.db.PutValidateReport(t.Context(), saved); err != nil {
		t.Fatal(err)
	}
	for _, part := range parts {
		if _, err = r.dispatchDelivery(t.Context(), part); err != nil {
			t.Fatal(err)
		}
	}
	for _, part := range parts {
		retained, err := o.db.GetReportDelivery(t.Context(), part.Key)
		if err != nil || !retained.Completed {
			t.Fatal("fixture parts must already be complete")
		}
	}
	if err = r.RecoverValidatePublications(t.Context(), o.instance.SessionID); err != nil {
		t.Fatal(err)
	}
	current, err := o.db.GetValidateReport(t.Context(), o.instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Digest == m.PartialDigest || current.Digest == m.PreviousDigest {
		t.Fatal("recovery stranded a partial index after all parts completed")
	}
	if len(o.forge.bodies("All numbered parts for this revision are confirmed")) != 1 {
		t.Fatal("recovery did not publish complete current index")
	}
}

func TestValidateGuidanceRepeatedPreparationReusesFrozenManifest(t *testing.T) {
	o, parts := validateOverflowFixture(t)
	r := o.opts.ResearchReporter
	st := o.state()
	st.DeliveryCommit = o.head()
	st.Findings = nil
	for i := 0; i < 400; i++ {
		st.Findings = append(st.Findings, ValidateFinding{ID: fmt.Sprintf("story-002:AC-%03d", i), Story: "story-002", Name: fmt.Sprintf("AC-%03d", i), State: findingAwaitingCheck, Status: "unverified", Criterion: true})
	}
	task, err := naming.ParseRef("github:acme/api#497")
	if err != nil {
		t.Fatal(err)
	}
	_, again, err := r.prepareValidateGuidance(t.Context(), o.instance.SessionID, task, st, o.b.Profile, testValidateLayout("Retained overview\n", st), "", o.clock.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != len(parts) {
		t.Fatal("replay changed layout")
	}
	for i := range parts {
		if again[i].Key != parts[i].Key || again[i].Body != parts[i].Body || again[i].Purpose != parts[i].Purpose {
			t.Fatal("replay changed immutable intent")
		}
	}
	if err = r.RecoverValidatePublications(t.Context(), o.instance.SessionID); err != nil {
		t.Fatal(err)
	}
	text, done, err := r.prepareValidateGuidance(t.Context(), o.instance.SessionID, task, st, o.b.Profile, testValidateLayout("Retained overview\n", st), "", o.clock.Add(2*time.Hour))
	if err != nil || len(done) != 0 || !strings.Contains(text, "All numbered parts") {
		t.Fatalf("settled replay did not reuse current index: %d %v", len(done), err)
	}
}

func TestValidateGuidanceRecoveryRestoresInterruptedIntentRetention(t *testing.T) {
	original, parts := validateOverflowFixture(t)
	restarted := newOwnerFixture(t)
	restarted.mixed()
	r := restarted.opts.ResearchReporter
	r.validateHead = func(context.Context, store.Session) (string, error) { return restarted.head(), nil }
	if original.instance.ID != restarted.instance.ID || original.instance.SessionID != restarted.instance.SessionID {
		t.Fatal("equivalent restart fixtures must use same retained identities")
	}
	// Only the first immutable intent reached durable storage before the crash.
	// Its manifest contains all remaining final redacted intents.
	if _, err := restarted.db.EnsureReportDelivery(t.Context(), parts[0]); err != nil {
		t.Fatal(err)
	}
	if err := r.RecoverValidatePublications(t.Context(), restarted.instance.SessionID); err != nil {
		t.Fatal(err)
	}
	for _, part := range parts {
		saved, err := restarted.db.GetReportDelivery(t.Context(), part.Key)
		if err != nil || !saved.Completed || saved.Body != part.Body {
			t.Fatalf("missing or changed recovered part: %v", err)
		}
	}
	if len(restarted.forge.bodies("All numbered parts for this revision are confirmed")) != 1 {
		t.Fatal("restarted publication did not complete current index")
	}
}

func TestValidateGuidanceSupersededUncertainEditDoesNotHoldCurrentIndex(t *testing.T) {
	o, oldParts := validateOverflowFixture(t)
	r := o.opts.ResearchReporter
	o.forge.editErr = errors.New("ambiguous old index response")
	for _, part := range oldParts {
		_, _ = r.dispatchDelivery(t.Context(), part)
	}
	o.forge.editErr = nil
	st := o.state()
	st.DeliveryCommit = o.head()
	st.Findings = nil
	for i := 0; i < 400; i++ {
		st.Findings = append(st.Findings, ValidateFinding{ID: fmt.Sprintf("story-002:AC-%03d", i), Story: "story-002", Name: fmt.Sprintf("AC-%03d", i), State: findingAwaitingCheck, Status: "unverified", Criterion: true})
	}
	task, err := naming.ParseRef("github:acme/api#497")
	if err != nil {
		t.Fatal(err)
	}
	text, currentParts, err := r.prepareValidateGuidance(t.Context(), o.instance.SessionID, task, st, o.b.Profile, testValidateLayout("New current overview\n", st), "", o.clock.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.ReportValidate(t.Context(), st.InstanceID, st.Revision, "", text, o.clock.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, part := range currentParts {
		if _, err = r.dispatchDelivery(t.Context(), part); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := o.db.ListSessionReportDeliveries(t.Context(), o.instance.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	var oldIndex store.ReportDelivery
	for _, d := range rows {
		if strings.HasPrefix(d.Purpose, validateIndexPurpose) && !d.Completed {
			oldIndex = d
			break
		}
	}
	if oldIndex.Key == "" || !oldIndex.PossiblySent || oldIndex.CommentID != 0 {
		t.Fatal("fixture requires an uncertain superseded edit")
	}
	before := o.forge.edits
	if err = r.RecoverValidatePublications(t.Context(), o.instance.SessionID); err != nil {
		t.Fatal("historical uncertainty held current owner work:", err)
	}
	retained, err := o.db.GetReportDelivery(t.Context(), oldIndex.Key)
	if err != nil {
		t.Fatal(err)
	}
	if retained.Completed || retained.CommentID != 0 || !retained.PossiblySent {
		t.Fatal("historical uncertainty was changed into invented publication evidence")
	}
	if o.forge.edits != before {
		t.Fatal("unchanged current index was unnecessarily edited")
	}
	// The old remote edit can finish after the current set was confirmed. Its
	// real receipt may settle, but the final projection must still be current.
	body, err := r.renderer.RenderReportDelivery(t.Context(), oldIndex.Body, "")
	if err != nil {
		t.Fatal(err)
	}
	current, _ := o.db.GetValidateReport(t.Context(), o.instance.ID)
	if err = o.forge.EditComment(t.Context(), github.Repo{Owner: "acme", Name: "api"}, current.CommentID, body); err != nil {
		t.Fatal(err)
	}
	if err = r.RecoverValidatePublications(t.Context(), o.instance.SessionID); err != nil {
		t.Fatal(err)
	}
	old, err := o.db.GetReportDelivery(t.Context(), oldIndex.Key)
	if err != nil || !old.Completed || old.CommentID == 0 {
		t.Fatal("observed late old receipt was not retained")
	}
	after, _ := o.db.GetValidateReport(t.Context(), o.instance.ID)
	if after.Digest != current.Digest || after.Revision != current.Revision {
		t.Fatal("late old receipt retargeted current guidance")
	}
	comments := o.forge.bodies("All numbered parts for this revision are confirmed")
	if len(comments) != 1 || !strings.Contains(comments[0].Body, "New current overview") {
		t.Fatal("late old edit left the provider parent stale")
	}
}

type validateQualifiedLinksForge struct{ *ownerForge }

func (f *validateQualifiedLinksForge) SourceLinks(_ context.Context, _ naming.Ref, _ int, replyID int64) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.comments {
		if c.ID == replyID {
			return "https://example.invalid/parent", c.HTMLURL, nil
		}
	}
	return "", "", errors.New("source reply missing")
}

func TestValidateGuidanceQualifiedPartMustRetainCompleteProviderBytes(t *testing.T) {
	for _, qualified := range []bool{false, true} {
		t.Run(fmt.Sprintf("qualified-%v", qualified), func(t *testing.T) {
			o, parts := validateOverflowFixture(t)
			r := o.opts.ResearchReporter
			if qualified {
				r.forge = &validateQualifiedLinksForge{ownerForge: o.forge}
			}
			for _, part := range parts[:len(parts)-1] {
				if _, err := r.dispatchDelivery(t.Context(), part); err != nil {
					t.Fatal(err)
				}
			}
			first, err := o.db.GetReportDelivery(t.Context(), parts[0].Key)
			if err != nil {
				t.Fatal(err)
			}
			// The provider still knows this exact destination and link, but the owner
			// can no longer reach every command at that edited destination.
			o.forge.mu.Lock()
			for i := range o.forge.comments {
				if o.forge.comments[i].ID == first.CommentID {
					o.forge.comments[i].Body = "edited part missing commands"
				}
			}
			o.forge.mu.Unlock()
			if _, err = r.dispatchDelivery(t.Context(), parts[len(parts)-1]); err == nil || !strings.Contains(err.Error(), "absent or changed") {
				t.Fatalf("changed provider part must prevent complete index: %v", err)
			}
			if len(o.forge.bodies("All numbered parts for this revision are confirmed")) != 0 {
				t.Fatal("incomplete provider content was advertised as complete")
			}
			if len(o.forge.bodies("Partial publication")) != 1 {
				t.Fatal("partial publication status was lost")
			}
			o.forge.mu.Lock()
			for i := range o.forge.comments {
				if o.forge.comments[i].ID == first.CommentID {
					o.forge.comments[i].Body = first.Body
				}
			}
			o.forge.mu.Unlock()
			if err = r.RecoverValidatePublications(t.Context(), o.instance.SessionID); err != nil {
				t.Fatal(err)
			}
			if len(o.forge.bodies("All numbered parts for this revision are confirmed")) != 1 {
				t.Fatal("restored exact provider bytes did not complete index")
			}
		})
	}
}

func TestValidateGuidanceRedactsSummaryBeforeClipping(t *testing.T) {
	o := newOwnerFixture(t)
	o.mixed()
	r := o.opts.ResearchReporter
	secret := "boundary-secret-value-ABCDEFGHIJKLMNOPQRSTUV"
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{"API_TOKEN=" + secret}}), Limit: github.CommentSizeLimit})
	if err != nil {
		t.Fatal(err)
	}
	r.renderer = renderer
	st := o.state()
	st.Findings = []ValidateFinding{{ID: "story-002:AC-002", Story: "story-002", Name: "AC-002", State: findingAwaitingCheck, Status: "unverified", Criterion: true, Summary: strings.Repeat("a", validateStoryTextFloor-16) + secret + " trailing evidence"}}
	task, err := naming.ParseRef("github:acme/api#497")
	if err != nil {
		t.Fatal(err)
	}
	text, _, err := r.prepareValidateGuidance(t.Context(), o.instance.SessionID, task, st, o.b.Profile, testValidateLayout("Overview\n", st), "", o.clock)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, secret[:12]) {
		t.Fatal("summary clipping exposed a secret prefix")
	}
	if !strings.Contains(text, ValidateCheckCommand+" "+st.Revision+" story-002:AC-002 pass -- <what you checked>") {
		t.Fatal("redaction damaged the current command")
	}
	if !strings.Contains(st.Findings[0].Summary, secret) {
		t.Fatal("projection mutated retained finding summary")
	}
}

func TestValidateAdmissionRefusalGivesOwnerHandoffAndNewRunEffect(t *testing.T) {
	v := newValidateAdmissionFixture(t)
	v.allMerged(t)
	out, handled, err := v.admit(t, v.request(8181, validateOwnerID+1, ValidateStartCommand))
	if err != nil || !handled || v.binds != 0 || v.validateInstances(t) != 0 {
		t.Fatalf("unauthorized start changed work: %v %+v", err, out)
	}
	for _, effect := range []string{"Only the owner", "configured owner", "`" + ValidateStartCommand + "`", "after Implement finishes", "does not authorize pushing"} {
		if !strings.Contains(out.Reason, effect) {
			t.Fatalf("missing handoff or effect %q: %s", effect, out.Reason)
		}
	}
	out, handled, err = v.admit(t, v.request(8182, validateOwnerID, "/validate-ship 0123456789ab -- reviewed"))
	if err != nil || !handled || v.binds != 0 || v.validateInstances(t) != 0 {
		t.Fatalf("unbound control changed work: %v %+v", err, out)
	}
	for _, effect := range []string{"No Validate run is waiting", "No current decision destination", "`" + ValidateStartCommand + "`", "new run tests and reviews", "does not approve delivery"} {
		if !strings.Contains(out.Reason, effect) {
			t.Fatalf("missing recovery or effect %q: %s", effect, out.Reason)
		}
	}
}

func TestValidateStoryChoicesAreValidCommandsWithEffects(t *testing.T) {
	st := ValidateState{Revision: "0123456789ab", Commit: strings.Repeat("a", 40), Findings: []ValidateFinding{{ID: "s:code", Story: "s", Name: "code", State: findingOpen, Blocking: true}, {ID: "s:AC-001", Story: "s", Name: "AC-001", State: findingAwaitingCheck, Status: "unverified", Criterion: true}}}
	body := renderValidateStoryComment(st, ValidateIssueRef{Key: "s"}, 497, "[current Validate report](https://example.invalid/report)")
	for _, command := range []string{"/validate-check 0123456789ab s:AC-001 pass -- <what you checked>", "/validate-check 0123456789ab s:AC-001 fail -- <what you saw>", "/validate-finding 0123456789ab s:code repair", "/validate-finding 0123456789ab s:code issue bug", "/validate-finding 0123456789ab s:code issue story", "/validate-finding 0123456789ab s:code accept -- <reason>"} {
		if !strings.Contains(body, command) {
			t.Fatalf("missing copyable choice %s", command)
		}
		if _, err := ParseValidateCommand(command); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Contains(body, "pass|fail") || strings.Contains(body, "repair|issue") {
		t.Fatal("story still offers invalid pipe grammar")
	}
	for _, effect := range []string{"owner supplies evidence", "fail keeps it blocking", "Repair and issue creation do not clear this blocker", "no delivery permission"} {
		if !strings.Contains(body, effect) {
			t.Fatalf("story lost choice consequence %q", effect)
		}
	}
}

func TestValidateGuidanceExplainsCombinedAndIndividualDeliveryAuthority(t *testing.T) {
	st := readyState()
	if next := st.NextStep(nil); !strings.Contains(next, ValidateShipCommand) || !strings.Contains(next, "one explicit reasoned command") || !strings.Contains(next, "then close") || !strings.Contains(next, "no additional confirmation") {
		t.Fatalf("combined shipping effect unclear: %s", next)
	}
	if next := st.deliveryCommand(nil); !strings.Contains(next, ValidatePushCommand) || !strings.Contains(next, "does not authorize merge or close") {
		t.Fatalf("individual push authority unclear: %s", next)
	}
	st.Effects.Push = vsEffect(1, store.EffectPush, "", store.EffectSucceeded, nil)
	st.Effects.PR = vsEffect(2, store.EffectPR, "", store.EffectSucceeded, map[string]any{"number": 42, "head": st.Commit})
	if next := st.deliveryCommand(nil); !strings.Contains(next, ValidateMergeCommand) || !strings.Contains(next, "does not authorize closing issues") {
		t.Fatalf("individual merge authority unclear: %s", next)
	}
	st.Effects.Merge = vsEffect(3, store.EffectMerge, "", store.EffectSucceeded, nil)
	if next := st.deliveryCommand([]int{497}); !strings.Contains(next, "closes exactly") || !strings.Contains(next, "after merge") {
		t.Fatalf("individual close effect unclear: %s", next)
	}
}

func TestValidateGuidanceClearedStoryDigestUsesOnlyObservedFrozenOverview(t *testing.T) {
	o, parts := validateOverflowFixture(t)
	r := o.opts.ResearchReporter
	saved, err := o.db.GetValidateReport(t.Context(), o.instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	saved.Digest = ""
	if err = o.db.PutValidateReport(t.Context(), saved); err != nil {
		t.Fatal(err)
	}
	for _, part := range parts {
		if _, err = r.dispatchDelivery(t.Context(), part); err != nil {
			t.Fatal(err)
		}
	}
	current, err := o.db.GetValidateReport(t.Context(), o.instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Digest != "" {
		t.Fatal("publication recovery consumed the pending story-refresh retry flag")
	}
	if len(o.forge.bodies("All numbered parts for this revision are confirmed")) != 1 {
		t.Fatal("observed frozen partial overview did not recover its complete index")
	}
}

func TestValidateGuidanceIndexRefusesChangedOutboundBytes(t *testing.T) {
	o, parts := validateOverflowFixture(t)
	r := o.opts.ResearchReporter
	o.forge.editErr = github.ErrRateLimited
	for _, part := range parts {
		_, _ = r.dispatchDelivery(t.Context(), part)
	}
	o.forge.editErr = nil
	rows, err := o.db.ListSessionReportDeliveries(t.Context(), o.instance.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	var index store.ReportDelivery
	for _, d := range rows {
		if strings.HasPrefix(d.Purpose, validateIndexPurpose) {
			index = d
		}
	}
	if index.Key == "" {
		t.Fatal("fixture lacks pending index")
	}
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{"API_TOKEN=Retained overview"}}), Limit: github.CommentSizeLimit})
	if err != nil {
		t.Fatal(err)
	}
	r.renderer = renderer
	before := o.forge.edits
	if _, err = r.dispatchDelivery(t.Context(), index); err == nil || !strings.Contains(err.Error(), "bytes changed") {
		t.Fatalf("changed outbound bytes were not refused: %v", err)
	}
	if o.forge.edits != before {
		t.Fatal("changed immutable index was published")
	}
	retained, err := o.db.GetReportDelivery(t.Context(), index.Key)
	if err != nil {
		t.Fatal(err)
	}
	if retained.Body != index.Body || retained.PossiblySent || retained.CommentID != 0 {
		t.Fatal("changed rendering altered retained intent or effect evidence")
	}
}

func TestValidateFailedCheckOffersRepeatVerdictsAndPermittedFindingChoices(t *testing.T) {
	st := ValidateState{Revision: "0123456789ab", Commit: strings.Repeat("a", 40), Findings: []ValidateFinding{{ID: "s:AC-001", Story: "s", Name: "AC-001", State: findingOwnerFailed, Status: "unverified", Criterion: true, Blocking: true}}}
	parent := strings.Join(validateDecisionItems(st), "\n")
	story := renderValidateStoryComment(st, ValidateIssueRef{Key: "s"}, 497)
	next := strings.Join(st.NextCommands(nil), "\n")
	for _, body := range []string{parent, story, next} {
		for _, command := range []string{"/validate-check 0123456789ab s:AC-001 pass -- <what you checked>", "/validate-check 0123456789ab s:AC-001 fail -- <what you saw>", "/validate-finding 0123456789ab s:AC-001 repair", "/validate-finding 0123456789ab s:AC-001 issue bug", "/validate-finding 0123456789ab s:AC-001 issue story"} {
			if !strings.Contains(body, command) {
				t.Fatalf("failed check hides legal current recovery %q: %s", command, body)
			}
			if _, err := ParseValidateCommand(command); err != nil {
				t.Fatal(err)
			}
		}
		if strings.Contains(body, "s:AC-001 accept") || strings.Contains(body, "s:AC-001 defer") {
			t.Fatal("failed criterion gained forbidden accept or defer")
		}
	}
}
