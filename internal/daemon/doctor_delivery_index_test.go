package daemon

import (
	"errors"
	"html"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/secrets"
)

func TestDoctorRecoveryRejectsConflictingDestinationAndBody(t *testing.T) {
	for _, defect := range []string{"destination", "body"} {
		t.Run(defect, func(t *testing.T) {
			d, forge, repo := doctorDaemonFixture(t)
			ctx := t.Context()
			if _, err := d.admitDoctor(ctx, repo, forge.issue, doctorComment(10, "/doctor apply")); err != nil {
				t.Fatal(err)
			}
			if err := d.doctorControls(ctx); err != nil {
				t.Fatal(err)
			}
			forge.postErr = errors.New("receipt lost")
			if err := d.publishDoctor(ctx); err == nil || len(forge.posts) != 1 {
				t.Fatalf("missing uncertain post: %v", err)
			}
			conflicting := forge.posts[0]
			if defect == "destination" {
				conflicting.HTMLURL = "https://github.com/acme/other/issues/406#issuecomment-100"
			} else {
				conflicting.Body = "changed diagnosis\n" + conflicting.Body
			}
			forge.seen = []github.Comment{forge.posts[0], conflicting}
			if err := d.publishDoctor(ctx); err == nil {
				t.Fatal("conflicting receipt was accepted")
			}
			pending, err := d.db.PendingDoctorDeliveries(ctx)
			if err != nil || len(pending) != 1 || len(forge.posts) != 1 {
				t.Fatalf("conflict completed or reposted: %+v %v", pending, err)
			}
		})
	}
}

func TestDoctorRateLimitRemainsSafelyRetryable(t *testing.T) {
	d, forge, repo := doctorDaemonFixture(t)
	ctx := t.Context()
	if _, err := d.admitDoctor(ctx, repo, forge.issue, doctorComment(10, "/doctor apply")); err != nil {
		t.Fatal(err)
	}
	if err := d.doctorControls(ctx); err != nil {
		t.Fatal(err)
	}
	forge.postErr = github.ErrRateLimited
	if err := d.publishDoctor(ctx); !errors.Is(err, github.ErrRateLimited) {
		t.Fatalf("rate limit disappeared: %v", err)
	}
	pending, err := d.db.PendingDoctorDeliveries(ctx)
	if err != nil || len(pending) != 1 || pending[0].PossiblySent || pending[0].Completed || pending[0].CommentID != 0 {
		t.Fatalf("rate-limit refusal became uncertain or completed: %+v %v", pending, err)
	}
}

func TestDoctorDeliveryRefusesNewlyKnownEscapedSecret(t *testing.T) {
	d, forge, repo := doctorDaemonFixture(t)
	ctx := t.Context()
	if _, err := d.admitDoctor(ctx, repo, forge.issue, doctorComment(10, "/doctor explain")); err != nil {
		t.Fatal(err)
	}
	command, err := d.db.ClaimDoctorCommand(ctx, "", false, "escaped-secret-owner")
	if err != nil {
		t.Fatal(err)
	}
	secret := "doctor-new-secret-<private>&value"
	body := "Unaccepted review\n<pre>" + html.EscapeString(secret) + "</pre>"
	if err = d.db.CompleteDoctorCommand(ctx, command.ID, body, 0); err != nil {
		t.Fatal(err)
	}
	d.doctor.service.Redactor.AddToken(secrets.NewToken(secret))
	if err = d.publishDoctor(ctx); err == nil {
		t.Fatal("changed redaction published as immutable retained intent")
	}
	if len(forge.posts) != 0 {
		t.Fatal("escaped secret sent to owner")
	}
	pending, err := d.db.PendingDoctorDeliveries(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending review: %+v %v", pending, err)
	}
	if pending[0].Completed || pending[0].PossiblySent || !strings.Contains(pending[0].Body, html.EscapeString(secret)) {
		t.Fatalf("immutable intent changed or delivery claimed: %+v", pending[0])
	}
}
