package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/analystschedule"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/loganalysis"
	"github.com/cloudvane/litefactory/internal/logevidence"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

// scheduleMainPathFixture creates local evidence and a verified book. Its
// controlled analyst exercises the real MCP boundary, not a live provider.
func scheduleMainPathFixture(t *testing.T) (config.Paths, store.Repo, store.Run, store.StandardsRevision, *analysisFixtureRunner) {
	t.Helper()
	paths := newRepoHome(t)
	const secret = "schedule-fixture-token-70391"
	t.Setenv("LF_SCHEDULE_FIXTURE_TOKEN", secret)
	writeInitConfig(t, paths, "providers:\n  github:\n    token_env: LF_SCHEDULE_FIXTURE_TOKEN\nagents:\n  claude: {}\n")
	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, Handle: "schedule-owner", UserID: 71, Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(paths.Home(), "repo"), Forge: store.ForgeGitHub, Owner: "fixture", Name: "analysis", RemoteURL: "git@github.com:fixture/analysis.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(paths.Home(), "worktrees")})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 99)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, 1, "analysis", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(paths.RunsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(paths.RunsDir(), "source.jsonl")
	sourceRun, err := db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "implement", Agent: store.AgentClaude, TriggerCommentID: 1, StartedAt: time.Now().Add(-time.Minute), LogPath: func(int) string { return logPath }})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(logPath, []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"A package check caught a regression before review. schedule-fixture-token-70391"}]}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = db.FinishRun(t.Context(), sourceRun.ID, store.RunOutcome{Status: store.RunSucceeded, FinishedAt: time.Now().Add(-time.Second)}); err != nil {
		t.Fatal(err)
	}
	// The newer completed run wins a one-run batch. Older terminal work
	// remains pending, while an active run is never eligible evidence.
	older, err := db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "implement", Agent: store.AgentClaude, TriggerCommentID: 2, StartedAt: time.Now().Add(-4 * time.Minute), LogPath: func(int) string { return filepath.Join(paths.RunsDir(), "older.jsonl") }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.FinishRun(t.Context(), older.ID, store.RunOutcome{Status: store.RunFailed, FinishedAt: time.Now().Add(-3 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "implement", Agent: store.AgentClaude, TriggerCommentID: 3, StartedAt: time.Now(), LogPath: func(int) string { return filepath.Join(paths.RunsDir(), "active.jsonl") }}); err != nil {
		t.Fatal(err)
	}
	bookService := &standards.Service{Store: db, OwnerID: 71}
	seed, err := bookService.Create(t.Context(), "schedule-seed", 0, []standards.Operation{{Kind: "category.add", Category: &standards.Category{ID: "quality", Name: "Quality"}}, {Kind: "standard.add", Standard: &standards.Standard{ID: "review", Title: "Review", Body: "Review changes before publication.", CategoryIDs: []string{"quality"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = bookService.Approve(t.Context(), seed.ProposalID, seed.Version, seed.ChangeDigest); err != nil {
		t.Fatal(err)
	}
	published, err := bookService.Publish(t.Context(), seed.ProposalID, seed.Version, seed.ChangeDigest)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	return paths, repo, sourceRun, published, &analysisFixtureRunner{t: t, runID: sourceRun.ID, secret: secret}
}

// This main path uses local fixtures and the restricted upstream MCP analyst.
// It makes no claim about a live provider or external delivery.
func TestAnalystScheduleEndToEndOptInRetainedNotices(t *testing.T) {
	paths, repo, source, published, analyst := scheduleMainPathFixture(t)
	runCommand := func(input string, args ...string) string {
		t.Helper()
		var out, diagnostics bytes.Buffer
		env := &Env{Stdout: &out, Stderr: &diagnostics, Stdin: strings.NewReader(input), Logger: slog.New(slog.DiscardHandler), Version: "fixture"}
		if code := NewRouter("litefactory", env, Commands()).Run(t.Context(), args); code != ExitOK {
			t.Fatalf("%v: exit %d: %s", args, code, diagnostics.String())
		}
		return out.String()
	}
	config, err := json.Marshal(analystschedule.Config{Name: "Recent review evidence", Kind: "logs", RepositoryID: repo.ID, IntervalSeconds: 3600, RunLimit: 1, LogLimits: loganalysis.Limits{Evidence: logevidence.Limits{Runs: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	created := analysisDecode[store.AnalystSchedule](t, []byte(runCommand(string(config), "standards", "schedule", "create", "--key", "recurring-main-path", "--config", "-", "--json")))
	if created.ID == "" || created.Enabled || analyst.calls != 0 {
		t.Fatalf("creation did not stay disabled: %+v calls=%d", created, analyst.calls)
	}
	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	authorizationChecks := 0
	svc := &analystschedule.Service{DB: db, Logs: &loganalysis.Service{DB: db, RunsRoot: paths.RunsDir(), Redactor: redact.New(redact.Options{Environ: os.Environ()}), Runner: analyst, Executable: executable, Model: "fixture-model"}, Authorize: func(_ context.Context, selected store.Repo, kind string) error {
		authorizationChecks++
		if selected.ID != repo.ID || kind != "logs" {
			return errors.New("unexpected scheduled authority")
		}
		return nil
	}}
	adapter := daemon.AnalystScheduleAdapter{Service: svc}
	if _, err = adapter.Dispatch(t.Context(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if analyst.calls != 0 {
		t.Fatal("disabled schedule launched analysis")
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	enabled := analysisDecode[store.AnalystSchedule](t, []byte(runCommand("", "standards", "schedule", "enable", created.ID, "--version", fmt.Sprint(created.Version), "--json")))
	if !enabled.Enabled || enabled.Version <= created.Version {
		t.Fatalf("explicit enable=%+v", enabled)
	}
	db, err = store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	svc.DB = db
	svc.Logs.DB = db
	dispatched, err := adapter.Dispatch(t.Context(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !dispatched || analyst.calls != 1 || authorizationChecks == 0 {
		t.Fatalf("dispatch=%v calls=%d authority=%d", dispatched, analyst.calls, authorizationChecks)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	// Reopening and polling the same nominal interval must reuse durable work.
	db, err = store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	svc.DB = db
	svc.Logs.DB = db
	if _, err = adapter.Dispatch(t.Context(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if analyst.calls != 1 {
		t.Fatalf("restart duplicated analysis: %d", analyst.calls)
	}
	receipt := analysisDecode[store.LogAnalysisReceipt](t, analyst.receipt.Result)
	if receipt.AnalysisID == "" || len(receipt.Proposals) != 1 {
		t.Fatalf("upstream receipt=%+v", receipt)
	}
	activityJSON := runCommand("", "standards", "schedule", "activity", created.ID, "--json")
	noticesJSON := runCommand("", "standards", "schedule", "notices", created.ID, "--json")
	for _, output := range []string{activityJSON, noticesJSON} {
		if !strings.Contains(output, receipt.AnalysisID) {
			t.Fatalf("retained output omitted analysis: %s", output)
		}
	}
	link := receipt.Proposals[0]
	activity := analysisDecode[[]store.AnalystOccurrence](t, []byte(activityJSON))
	notices := analysisDecode[[]store.AnalystProposalNotice](t, []byte(noticesJSON))
	if len(activity) != 1 || len(activity[0].Works) != 1 || activity[0].Works[0].RunID != source.ID || !activity[0].MorePending || activity[0].Disposition != "proposals" {
		t.Fatalf("bounded recent-run activity=%+v", activity)
	}
	if len(notices) != 1 || notices[0].AnalysisID != receipt.AnalysisID || notices[0].OccurrenceID != activity[0].ID || notices[0].ScheduleID != created.ID {
		t.Fatalf("retained notice linkage=%+v", notices)
	}
	if notices[0].ProposalID != link.ProposalID || notices[0].ProposalVersion != link.Version || notices[0].BaseRevision != link.BaseRevision || notices[0].ChangeDigest != link.ChangeDigest || notices[0].ResultDigest != link.ResultDigest || notices[0].OriginStatus != "unapproved" {
		t.Fatalf("notice changed exact original proposal: %+v; link=%+v", notices[0], link)
	}
	shownNotice := analysisDecode[store.AnalystProposalNotice](t, []byte(runCommand("", "standards", "schedule", "notices", "show", fmt.Sprint(notices[0].ID), "--json")))
	if shownNotice != notices[0] {
		t.Fatalf("notice show changed retained reference: %+v", shownNotice)
	}
	for _, want := range []string{link.ProposalID, link.ChangeDigest, link.ResultDigest, "unapproved"} {
		if !strings.Contains(noticesJSON, want) {
			t.Fatalf("retained notice missing %q: %s", want, noticesJSON)
		}
	}
	for _, want := range []string{"proposals", fmt.Sprint(source.ID), "more_pending"} {
		if !strings.Contains(activityJSON, want) {
			t.Fatalf("activity missing %q: %s", want, activityJSON)
		}
	}
	shown := runCommand("", "standards", "schedule", "show", created.ID, "--json")
	if !strings.Contains(shown, created.ID) {
		t.Fatalf("show omitted schedule: %s", shown)
	}
	activityText := runCommand("", "standards", "schedule", "activity", created.ID)
	noticesText := runCommand("", "standards", "schedule", "notices", created.ID)
	if !strings.Contains(activityText, receipt.AnalysisID) || !strings.Contains(noticesText, link.ProposalID) {
		t.Fatalf("text inspection lost retained references: activity=%s notices=%s", activityText, noticesText)
	}
	reopened, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	exact, err := reopened.GetStandardsProposal(t.Context(), link.ProposalID, link.Version)
	if err != nil {
		t.Fatal(err)
	}
	if link.Version != 1 || link.BaseRevision != published.ID || exact.ChangeDigest != link.ChangeDigest || exact.ResultDigest != link.ResultDigest {
		t.Fatalf("notice proposal was not exact original version: %+v %+v", link, exact)
	}
	if _, err = reopened.GetStandardsApproval(t.Context(), link.ProposalID, link.Version); !errors.Is(err, store.ErrStandardsApprovalRequired) {
		t.Fatalf("scheduled proposal gained approval: %v", err)
	}
	head, err := reopened.StandardsHead(t.Context())
	if err != nil || head != published.ID {
		t.Fatalf("scheduler published standards: head=%d error=%v", head, err)
	}
	if analyst.calls != 1 {
		t.Fatalf("inspection relaunched analyst: %d", analyst.calls)
	}
}
