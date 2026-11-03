package store

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// This fixture is created by the pre-extraction commit through public storage
// operations. The rehearsal script copies this test into an archived baseline.
const discordFixtureBaseline = "d41b68afd2f722003d0d12efe396801be4195367"

var discordFixtureTime = time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC)

type discordFixtureSnapshot struct {
	Baseline      string
	Conversations []Conversation
	Captures      [][]byte
	Turns         []ConversationTurn
	Answer        ConversationAnswer
	Parts         []ConversationAnswerPart
	Proposal      OwnerAction
	Schema        []string
}

func discordFixtureRequire(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func discordFixtureRead(t *testing.T, db *DB) discordFixtureSnapshot {
	t.Helper()
	ctx := t.Context()
	m := discordFixtureSnapshot{Baseline: discordFixtureBaseline}
	for _, channel := range []string{"partial", "unfinished"} {
		c, err := db.ReadConversation(ctx, "discord", channel)
		discordFixtureRequire(t, err)
		m.Conversations = append(m.Conversations, c)
		capture, legacy, err := db.ReadConversationCapture(ctx, c.ID)
		discordFixtureRequire(t, err)
		if legacy {
			t.Fatal("fixture unexpectedly became legacy")
		}
		m.Captures = append(m.Captures, capture)
		turn, err := db.LatestConversationTurn(ctx, c.ID)
		discordFixtureRequire(t, err)
		m.Turns = append(m.Turns, turn)
	}
	var err error
	m.Answer, err = db.ReadConversationAnswer(ctx, "discord", "100", "partial", "saved-answer")
	discordFixtureRequire(t, err)
	m.Parts, err = db.ReadConversationAnswerParts(ctx, m.Answer.ID)
	discordFixtureRequire(t, err)
	m.Proposal, err = db.ReadOwnerAction(ctx, "discord-pending")
	discordFixtureRequire(t, err)
	rows, err := db.sql.QueryContext(ctx, `SELECT type||':'||name||':'||COALESCE(sql,'') FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' ORDER BY type,name`)
	discordFixtureRequire(t, err)
	defer rows.Close()
	for rows.Next() {
		var line string
		discordFixtureRequire(t, rows.Scan(&line))
		m.Schema = append(m.Schema, line)
	}
	discordFixtureRequire(t, rows.Err())
	return m
}

func discordFixtureCreate(t *testing.T, db *DB) {
	t.Helper()
	ctx, at := t.Context(), discordFixtureTime
	for _, channel := range []string{"partial", "unfinished"} {
		c, err := db.EnsureConversationWithCapture(ctx, Conversation{Transport: "discord", ChannelRef: channel, OwnerID: "100", Name: channel, Provider: "claude", StateDir: "/offline-discord/" + channel}, []byte(`{"fixture":"immutable retained capture","version":1}`), at)
		discordFixtureRequire(t, err)
		discordFixtureRequire(t, db.ActivateNaturalScan(ctx, channel, "100"))
		scan, err := db.ReadNaturalScan(ctx, channel)
		discordFixtureRequire(t, err)
		discordFixtureRequire(t, db.StageNaturalScan(ctx, scan, NaturalScan{ChannelRef: channel, Cursor: "100", HighWater: "200"}, []NaturalMessage{{ChannelRef: channel, SourceID: "200", OwnerID: "100", Text: "original owner input", CreatedAt: at}}, true))
		_, err = db.AdmitConversationTurn(ctx, c.ID, "200", "saved original envelope", at)
		discordFixtureRequire(t, err)
		turn, err := db.ClaimConversationTurn(ctx, c.ID, "baseline-worker", at, time.Minute)
		discordFixtureRequire(t, err)
		discordFixtureRequire(t, db.SaveConversationSession(ctx, turn, "native-session-"+channel))
		discordFixtureRequire(t, db.SaveConversationExchange(ctx, turn, ConversationExchange{TurnID: turn.ID, Ordinal: 1, Kind: "provider", Request: "saved provider input", CapturedAt: at, ProviderAttempt: "baseline-provider"}))
		if channel == "partial" {
			discordFixtureRequire(t, db.CompleteConversationAnswer(ctx, turn, "completed", ConversationAnswer{ID: "saved-answer", Blocks: `[{"text":"saved answer"}]`, Version: 1}, []ConversationAnswerPart{{Text: "first saved bytes"}, {Text: "second saved bytes"}, {Text: "third saved bytes"}}, at))
			reply, err := db.ClaimConversationReply(ctx, "discord", at)
			discordFixtureRequire(t, err)
			discordFixtureRequire(t, db.FinishConversationReply(ctx, reply, "provider-message-1", at))
		}
	}
	owner := connectorTestOwner(t, db, 42)
	discordFixtureRequire(t, db.CreateOwnerAction(ctx, OwnerAction{ID: "discord-pending", PolicyVersion: 1, Principal: OwnerActionPrincipal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "discord", ChannelID: "partial", DiscordOwnerID: "100"}, RequestKey: "discord-key", RequestDigest: "protected-request", Facts: `{"revision":"saved"}`, Payload: `{"exact":"saved protected payload"}`, Digest: "saved-digest", Scope: "standards:management", Tier: "conversational", State: "awaiting_confirmation", CreatedAt: at, AvailableAt: at, ConfirmNotBefore: at.Add(5 * time.Second), ExpiresAt: at.Add(15 * time.Minute), ExecutionID: "owner-action:discord-pending"}))
}

func discordFixtureRecover(t *testing.T, db *DB, before discordFixtureSnapshot) {
	t.Helper()
	ctx, at := t.Context(), discordFixtureTime.Add(2*time.Minute)
	// The baseline process has exited. Recovery observes its exact fenced turn;
	// elapsed lease by itself is not evidence that a native process has exited.
	running, err := db.RecoverConversationTurns(ctx, before.Conversations[1].ID, at)
	discordFixtureRequire(t, err)
	if len(running) != 1 || !reflect.DeepEqual(running[0], before.Turns[1]) {
		t.Fatalf("unfinished identity changed: %+v", running)
	}
	for _, want := range []string{"second saved bytes", "third saved bytes"} {
		reply, err := db.ClaimConversationReply(ctx, "discord", at)
		discordFixtureRequire(t, err)
		if reply.ReplyText != want {
			t.Fatalf("saved order/bytes: %q, want %q", reply.ReplyText, want)
		}
		discordFixtureRequire(t, db.FinishConversationReply(ctx, reply, "provider-message-"+want, at))
		if err := db.FinishConversationReply(ctx, reply, "duplicate", at); !errors.Is(err, ErrNotFound) {
			t.Fatalf("accepted stale acknowledgment: %v", err)
		}
	}
	if _, err := db.ClaimConversationReply(ctx, "discord", at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("replayed completed delivery: %v", err)
	}
	after := discordFixtureRead(t, db)
	if !reflect.DeepEqual(after.Proposal, before.Proposal) || !reflect.DeepEqual(after.Captures, before.Captures) || !reflect.DeepEqual(after.Conversations, before.Conversations) || !reflect.DeepEqual(after.Turns[1], before.Turns[1]) || !reflect.DeepEqual(after.Schema, before.Schema) {
		t.Fatal("delivery recovery changed protected pending state")
	}
	if _, err := db.ReadNaturalExecution(ctx, before.Proposal.ExecutionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("restart authorized pending approval: %v", err)
	}
	if after.Answer.CompletedAt.IsZero() {
		t.Fatal("partial answer did not complete")
	}
}

// discordFixtureDirectory accepts only an explicitly isolated temporary directory.
func discordFixtureDirectory(t *testing.T, variable string) string {
	t.Helper()
	directory := os.Getenv(variable)
	if directory == "" {
		return ""
	}
	if !filepath.IsAbs(directory) {
		t.Fatal("fixture directory must be absolute")
	}
	directory = filepath.Clean(directory)
	resolved, err := filepath.EvalSymlinks(directory)
	discordFixtureRequire(t, err)
	for _, root := range []string{os.TempDir(), "/tmp"} {
		root, err = filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		relative, err := filepath.Rel(root, resolved)
		if err == nil && relative != "." && filepath.IsLocal(relative) {
			return filepath.Clean(resolved)
		}
	}
	t.Fatal("fixture directory must be below a temporary directory")
	return ""
}

func TestDiscordComponentBaselineRecovery(t *testing.T) {
	operation := os.Getenv("LF_DISCORD_FIXTURE_OPERATION")
	directory := discordFixtureDirectory(t, "LF_DISCORD_FIXTURE_DIRECTORY")
	path := filepath.Join(directory, "pending.db")
	manifest := filepath.Join(directory, "manifest.json")
	if operation == "" {
		directory = t.TempDir()
		path = filepath.Join(directory, "pending.db")
		input, err := os.Open("testdata/discord-component-recovery/pending.db.gz")
		discordFixtureRequire(t, err)
		defer input.Close()
		reader, err := gzip.NewReader(input)
		discordFixtureRequire(t, err)
		data, err := io.ReadAll(reader)
		discordFixtureRequire(t, err)
		discordFixtureRequire(t, reader.Close())
		discordFixtureRequire(t, os.WriteFile(path, data, 0600))
		manifest = "testdata/discord-component-recovery/manifest.json"
	}
	db, err := Open(t.Context(), path)
	discordFixtureRequire(t, err)
	defer func() { discordFixtureRequire(t, db.Close()) }()
	discordFixtureRequire(t, db.Migrate(t.Context()))
	if operation == "create" {
		discordFixtureCreate(t, db)
		data, err := json.MarshalIndent(discordFixtureRead(t, db), "", "  ")
		discordFixtureRequire(t, err)
		discordFixtureRequire(t, os.WriteFile(manifest, append(data, '\n'), 0600))
		return
	}
	data, err := os.ReadFile(manifest)
	discordFixtureRequire(t, err)
	var expected discordFixtureSnapshot
	discordFixtureRequire(t, json.Unmarshal(data, &expected))
	actual := discordFixtureRead(t, db)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatal("baseline state or schema changed on reopen")
	}
	if operation == "verify" {
		return
	}
	discordFixtureRecover(t, db, expected)
	if operation == "recover" {
		data, err := json.MarshalIndent(discordFixtureRead(t, db), "", "  ")
		discordFixtureRequire(t, err)
		discordFixtureRequire(t, os.WriteFile(manifest, append(data, '\n'), 0600))
	}
}

// The rehearsal invokes this reader under archived previous code between the
// first component stop/drain and new-code resume, then again after delivery.
func TestDiscordComponentProcessRollback(t *testing.T) {
	directory := discordFixtureDirectory(t, "LF_DISCORD_PROCESS_EXPORT")
	if directory == "" {
		t.Skip("requires the isolated process rehearsal database")
	}
	data, err := os.ReadFile(filepath.Join(directory, "process-manifest.json"))
	discordFixtureRequire(t, err)
	var expected struct {
		Conversation      Conversation
		Capture           []byte
		Turn              ConversationTurn
		Answer            ConversationAnswer
		Parts             []ConversationAnswerPart
		Reply             ChatReply
		Proposal          OwnerAction
		Unfinished        Conversation
		UnfinishedCapture []byte
		UnfinishedTurn    ConversationTurn
	}
	discordFixtureRequire(t, json.Unmarshal(data, &expected))
	db, err := Open(t.Context(), filepath.Join(directory, "pending.db"))
	discordFixtureRequire(t, err)
	defer func() { discordFixtureRequire(t, db.Close()) }()
	discordFixtureRequire(t, db.Migrate(t.Context()))
	actual := expected
	actual.Conversation, err = db.ReadConversation(t.Context(), "discord", "200")
	discordFixtureRequire(t, err)
	actual.Capture, _, err = db.ReadConversationCapture(t.Context(), actual.Conversation.ID)
	discordFixtureRequire(t, err)
	actual.Turn, err = db.LatestConversationTurn(t.Context(), actual.Conversation.ID)
	discordFixtureRequire(t, err)
	actual.Answer, err = db.ReadConversationAnswer(t.Context(), "discord", "100", "200", "process-answer")
	discordFixtureRequire(t, err)
	actual.Parts, err = db.ReadConversationAnswerParts(t.Context(), "process-answer")
	discordFixtureRequire(t, err)
	if len(actual.Parts) != 2 {
		t.Fatal("process snapshot must retain two answer parts")
	}
	actual.Reply, err = db.ReadChatReply(t.Context(), "discord", actual.Parts[1].ReplyID)
	discordFixtureRequire(t, err)
	actual.Proposal, err = db.ReadOwnerAction(t.Context(), "process-approval")
	discordFixtureRequire(t, err)
	actual.Unfinished, err = db.ReadConversation(t.Context(), "discord", "201")
	discordFixtureRequire(t, err)
	actual.UnfinishedCapture, _, err = db.ReadConversationCapture(t.Context(), actual.Unfinished.ID)
	discordFixtureRequire(t, err)
	actual.UnfinishedTurn, err = db.LatestConversationTurn(t.Context(), actual.Unfinished.ID)
	discordFixtureRequire(t, err)
	if _, err = db.ReadNaturalExecution(t.Context(), actual.Proposal.ExecutionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rollback authorized pending approval: %v", err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatal("compatible previous code changed process recovery state")
	}
	if actual.Answer.CompletedAt.IsZero() {
		if actual.Parts[0].AcknowledgedAt.IsZero() || !actual.Parts[1].AcknowledgedAt.IsZero() || actual.Reply.State != "pending" {
			t.Fatal("rollback lost partial pending delivery")
		}
		t.Log("previous code preserved pending reply bytes, acknowledgment, native identities, unfinished turn and approval")
	} else if _, err := db.ClaimConversationReply(t.Context(), "discord", time.Now().UTC()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rollback replayed a completed process delivery: %v", err)
	} else {
		t.Log("previous code preserved completed delivery fence, pending approval and unfinished conversation")
	}
}
