package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestDoctorCommand_Arguments(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		args    []string
		message string
		success bool
	}{
		{"diagnose missing target/rejected", []string{"doctor"}, "repository", false},
		{"diagnose missing instructions/rejected", []string{"doctor", "owner/repo", "406"}, "instructions", false},
		{"diagnose invalid issue/rejected", []string{"doctor", "owner/repo", "wrong", "diagnose"}, "positive integer", false},
		{"diagnose zero issue/rejected", []string{"doctor", "owner/repo", "0", "diagnose"}, "positive", false},
		{"diagnose invalid repository/rejected", []string{"doctor", "../repo", "406", "diagnose"}, "relative path", false},
		{"status missing target/rejected", []string{"doctor", "status"}, "repository", false},
		{"stop missing issue/rejected", []string{"doctor", "stop", "owner/repo"}, "issue", false},
		{"status extra instructions/rejected", []string{"doctor", "status", "owner/repo", "406", "extra"}, "usage", false},
		{"diagnosis help/explicit target", []string{"doctor", "--help"}, "<repository> <issue> <instructions>", true},
		{"status help/explicit target", []string{"doctor", "status", "--help"}, "<repository> <issue>", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			router, out, errout := newTestRouter(t)
			code := router.Run(t.Context(), tt.args)
			if (code == ExitOK) != tt.success || !strings.Contains(strings.ToLower(out.String()+errout.String()), strings.ToLower(tt.message)) {
				t.Fatalf("exit=%d stdout=%s stderr=%s; want success=%t and %q", code, out, errout, tt.success, tt.message)
			}
		})
	}
}

// doctor apply takes exactly a repository and an issue and is dispatched to its
// handler. LF_HOME is pointed at an empty directory so the handler fails on the
// missing configuration instead of reading the operator's real ~/.litefactory,
// which a sandboxed check cannot read and whose contents vary per machine.
func TestDoctorApplyReachesItsHandler(t *testing.T) {
	// No t.Parallel: t.Setenv forbids it.
	t.Setenv(config.EnvHome, t.TempDir())

	router, _, _ := newTestRouter(t)
	err := router.dispatch(t.Context(), []string{"doctor", "apply", "owner/repo", "406"})
	if err == nil {
		t.Fatal("doctor apply succeeded against an empty LF_HOME, so it cannot have reached the handler")
	}
	var usageErr *UsageError
	if errors.As(err, &usageErr) {
		t.Fatalf("doctor apply returned the usage error %q instead of reaching its handler", err)
	}
}

func TestDoctorStopCLIReportsOnlyLocally(t *testing.T) {
	for _, shape := range []string{"workflow", "doctor", "both", "neither"} {
		t.Run(shape, func(t *testing.T) {
			paths := newRepoHome(t)
			ctx := t.Context()
			check := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			db, err := store.Open(ctx, paths.DBPath())
			check(err)
			defer db.Close()
			repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
			check(err)
			if shape == "workflow" || shape == "both" {
				session, err := db.GetOrCreateSession(ctx, repo, 406)
				check(err)
				segment, err := db.GetOrCreateSegment(ctx, session.ID, 1, "implement", store.AgentClaude)
				check(err)
				_, err = db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "implement", Agent: store.AgentClaude, LogPath: func(int) string { return "test.log" }})
				check(err)
			}
			if shape == "doctor" || shape == "both" {
				_, err := db.BeginDoctorAttempt(ctx, "github:acme/api#406", store.DoctorAttempt{ID: "active", SourceID: "test:active", OwnerText: "diagnose", PromptVersion: "test", GuideVersion: "test"})
				check(err)
			}
			router, out, errout := newTestRouter(t)
			code := router.Run(ctx, []string{"doctor", "stop", "acme/api", "406"})
			if code != ExitOK {
				t.Fatalf("exit=%d output=%s %s", code, out, errout)
			}
			want := "Nothing was running"
			if shape == "doctor" {
				want = "Doctor conversation stopped"
			}
			if shape == "workflow" || shape == "both" {
				want = "cancellation and saving are pending"
			}
			if !strings.Contains(out.String(), want) {
				t.Fatal(out.String())
			}
			if shape == "both" && !strings.Contains(out.String(), "Doctor conversation stopped: true") {
				t.Fatal(out.String())
			}
			debts, err := db.PendingDoctorDeliveries(ctx)
			check(err)
			commands, err := db.DoctorCommandsNeedingReports(ctx)
			check(err)
			if len(debts) != 0 || len(commands) != 0 {
				t.Fatal("CLI created issue report debt")
			}
		})
	}
}

func TestDoctorLocalPresentationUsesCurrentCapabilityAndSettlement(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, state, want string
		available         bool
	}{
		{"proposal", "", "litefactory doctor apply owner/repo 47", true},
		{"unverified", "", "Apply capability was not verified for this readout", false},
		{"unconfirmed intermediate phase", "applying", "does not confirm current execution", true},
		{"settled", "completed", "litefactory logs --all --grep 'github:owner/repo#47'", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := doctor.Conversation{Conversation: store.DoctorConversation{ID: 7, TaskRef: "github:owner/repo#47"}, ApplyAvailable: tc.available}
			if tc.name == "proposal" {
				c.CurrentProposal = []doctor.RepairStep{{Number: 1, Target: "run:7"}}
			}
			if tc.state != "" {
				c.Repair = &store.DoctorRepair{ID: "retained-7", TaskRef: c.Conversation.TaskRef, State: tc.state}
			}
			var out bytes.Buffer
			if err := renderDoctor(&out, c); err != nil {
				t.Fatal(err)
			}
			text := out.String()
			if !strings.Contains(text, tc.want) || strings.Contains(text, "Repeat doctor") {
				t.Fatalf("incorrect local next step: %s", text)
			}
			if (tc.available || tc.state != "") && strings.Contains(text, "Apply is unavailable") {
				t.Fatalf("available or retained repair misreported: %s", text)
			}
		})
	}
}
