package discordcomponent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/discordcomponent"
	"github.com/cloudvane/litefactory/internal/factory"
	"github.com/cloudvane/litefactory/internal/factorylib/factorylibtest"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messagingstorage"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

func processRequire(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// A real child process runs the extracted component against a local fake Discord
// server. Interrupt is handled as the daemon's cancellation signal, and the
// child must join Serve before closing the shared database and exiting.
func TestDiscordComponentProcessStopDrainRestart(t *testing.T) {
	if os.Getenv("LF_DISCORD_PROCESS_CHILD") == "1" {
		runDiscordProcessChild(t)
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	directory := os.Getenv("LF_DISCORD_PROCESS_EXPORT")
	if directory == "" {
		directory = t.TempDir()
	} else if !filepath.IsAbs(directory) {
		t.Fatal("process export must be an isolated absolute directory")
	}
	database := filepath.Join(directory, "pending.db")
	db, err := store.Open(ctx, database)
	processRequire(t, err)
	defer func() {
		if db != nil {
			processRequire(t, db.Close())
		}
	}()
	processRequire(t, db.Migrate(ctx))
	capture, err := factorylibtest.Library(t).CaptureConversation(ctx)
	processRequire(t, err)
	raw, err := capture.Encode()
	processRequire(t, err)
	at := time.Now().UTC()
	conversation, err := db.EnsureConversationWithCapture(ctx, store.Conversation{Transport: "discord", ChannelRef: "200", OwnerID: "100", Name: "saved", Provider: "claude", StateDir: filepath.Join(directory, "native")}, raw, at)
	processRequire(t, err)
	processRequire(t, db.ActivateNaturalScan(ctx, "200", "100"))
	scan, err := db.ReadNaturalScan(ctx, "200")
	processRequire(t, err)
	processRequire(t, db.StageNaturalScan(ctx, scan, store.NaturalScan{ChannelRef: "200", Cursor: "100", HighWater: "300"}, []store.NaturalMessage{{ChannelRef: "200", SourceID: "300", OwnerID: "100", Text: "retained input", CreatedAt: at}}, true))
	_, err = db.AdmitConversationTurn(ctx, conversation.ID, "300", "retained envelope", at)
	processRequire(t, err)
	turn, err := db.ClaimConversationTurn(ctx, conversation.ID, "previous-process", at, time.Minute)
	processRequire(t, err)
	processRequire(t, db.SaveConversationSession(ctx, turn, "retained-native-session"))
	processRequire(t, db.CompleteConversationAnswer(ctx, turn, "completed", store.ConversationAnswer{ID: "process-answer", Version: 1, Blocks: "[]"}, []store.ConversationAnswerPart{{Text: "first saved bytes"}, {Text: "second saved bytes"}}, at))
	unfinished, err := db.EnsureConversationWithCapture(ctx, store.Conversation{Transport: "discord", ChannelRef: "201", OwnerID: "100", Name: "unfinished", Provider: "claude", StateDir: filepath.Join(directory, "unfinished-native")}, raw, at)
	processRequire(t, err)
	processRequire(t, db.ActivateNaturalScan(ctx, "201", "100"))
	unfinishedScan, err := db.ReadNaturalScan(ctx, "201")
	processRequire(t, err)
	processRequire(t, db.StageNaturalScan(ctx, unfinishedScan, store.NaturalScan{ChannelRef: "201", Cursor: "100", HighWater: "301"}, []store.NaturalMessage{{ChannelRef: "201", SourceID: "301", OwnerID: "100", Text: "unfinished retained input", CreatedAt: at}}, true))
	_, err = db.AdmitConversationTurn(ctx, unfinished.ID, "301", "unfinished retained envelope", at)
	processRequire(t, err)
	unfinishedTurn, err := db.ClaimConversationTurn(ctx, unfinished.ID, "stopped-fixture-worker", at, time.Hour)
	processRequire(t, err)
	processRequire(t, db.SaveConversationSession(ctx, unfinishedTurn, "unfinished-native-session"))
	_, err = db.UpsertIdentity(ctx, store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH})
	processRequire(t, err)
	owner, err := db.ConnectorOwner(ctx, 42)
	processRequire(t, err)
	processRequire(t, db.CreateOwnerAction(ctx, store.OwnerAction{ID: "process-approval", PolicyVersion: 1, Principal: store.OwnerActionPrincipal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "discord", ChannelID: "200", DiscordOwnerID: "100"}, RequestKey: "process-request", RequestDigest: "protected-request", Facts: `{"revision":"saved"}`, Payload: `{"exact":"saved protected payload"}`, Digest: "saved-digest", Scope: "standards:management", Tier: "conversational", State: "awaiting_confirmation", CreatedAt: at, AvailableAt: at, ConfirmNotBefore: at.Add(5 * time.Second), ExpiresAt: at.Add(15 * time.Minute), ExecutionID: "owner-action:process-approval"}))
	var mu sync.Mutex
	restarted := false
	var nonces []string
	firstSends, secondSends := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/channels/200/messages" {
			var payload struct {
				Content, Nonce string
				Enforce        bool `json:"enforce_nonce"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if !payload.Enforce || payload.Nonce == "" {
				t.Error("missing enforced stable nonce")
			}
			switch payload.Content {
			case "first saved bytes":
				firstSends++
				_, _ = io.WriteString(w, `{"id":"901"}`)
			case "second saved bytes":
				secondSends++
				nonces = append(nonces, payload.Nonce)
				if !restarted {
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = io.WriteString(w, `{"retry_after":1}`)
				} else {
					_, _ = io.WriteString(w, `{"id":"902"}`)
				}
			default:
				t.Errorf("unexpected provider effect: %q", payload.Content)
				w.WriteHeader(http.StatusBadRequest)
			}
			return
		}
		switch r.URL.Path {
		case "/applications/@me":
			_, _ = io.WriteString(w, `{"flags":262144}`)
		case "/gateway/bot":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{}`)
		default:
			_, _ = io.WriteString(w, `[]`)
		}
	}))
	defer server.Close()
	start := func(phase string) *exec.Cmd {
		t.Helper()
		executable, err := os.Executable()
		processRequire(t, err)
		command := exec.CommandContext(ctx, executable, "-test.run=^TestDiscordComponentProcessStopDrainRestart$", "-test.timeout=30s")
		command.Env = append(os.Environ(), "LF_DISCORD_PROCESS_CHILD=1", "TEST_BASE_URL="+server.URL, "TEST_DATABASE_URL="+database, "LF_DISCORD_PROCESS_DIRECTORY="+directory, "LF_DISCORD_PROCESS_PHASE="+phase)
		output, err := os.Create(filepath.Join(directory, phase+".log"))
		processRequire(t, err)
		t.Cleanup(func() { processRequire(t, output.Close()) })
		command.Stdout = output
		command.Stderr = output
		processRequire(t, command.Start())
		t.Cleanup(func() {
			if command.ProcessState == nil {
				_ = command.Process.Kill()
				_ = command.Wait()
			}
		})
		return command
	}
	wait := func(condition func() bool) {
		t.Helper()
		timer := time.NewTicker(10 * time.Millisecond)
		defer timer.Stop()
		for !condition() {
			select {
			case <-ctx.Done():
				t.Fatal("process rehearsal timed out")
			case <-timer.C:
			}
		}
	}
	stop := func(command *exec.Cmd, phase string) {
		t.Helper()
		processRequire(t, command.Process.Signal(os.Interrupt))
		if err := command.Wait(); err != nil {
			data, _ := os.ReadFile(filepath.Join(directory, phase+".log"))
			t.Fatalf("component process: %v\n%s", err, data)
		}
		data, err := os.ReadFile(filepath.Join(directory, phase+".drained"))
		processRequire(t, err)
		if string(data) != "joined and admission stopped" {
			t.Fatal("missing completed drain")
		}
	}
	t.Log("phase: start new-code component with partial reply, unfinished conversation and pending approval")
	first := start("before")
	wait(func() bool {
		parts, err := db.ReadConversationAnswerParts(ctx, "process-answer")
		if err != nil || len(parts) != 2 || parts[0].AcknowledgedAt.IsZero() {
			return false
		}
		reply, err := db.ReadChatReply(ctx, "discord", parts[1].ReplyID)
		return err == nil && reply.State == "pending" && reply.Attempts > 0
	})
	stop(first, "before")
	t.Log("phase: first new-code component stopped and drained")
	parts, err := db.ReadConversationAnswerParts(ctx, "process-answer")
	processRequire(t, err)
	if parts[0].AcknowledgedAt.IsZero() || !parts[1].AcknowledgedAt.IsZero() {
		t.Fatal("drain lost partial acknowledgment")
	}
	pending := readProcessSnapshot(t, db)
	if pending.Proposal.State != "awaiting_confirmation" || pending.UnfinishedTurn.State != "running" || !pending.Answer.CompletedAt.IsZero() {
		t.Fatal("rollback must begin with pending approval, unfinished turn and partial delivery")
	}
	writeProcessSnapshot(t, directory, "pending-manifest.json", pending)
	writeProcessSnapshot(t, directory, "process-manifest.json", pending)
	if baseline := os.Getenv("LF_DISCORD_BASELINE_READER"); baseline != "" {
		processRequire(t, db.Close())
		db = nil
		reader := exec.CommandContext(ctx, baseline, "-test.run=^TestDiscordComponentProcessRollback$", "-test.timeout=20s", "-test.v")
		reader.Env = append(os.Environ(), "LF_DISCORD_PROCESS_EXPORT="+directory)
		output, runErr := reader.CombinedOutput()
		processRequire(t, os.WriteFile(filepath.Join(directory, "baseline-pending-read.log"), output, 0600))
		if runErr != nil {
			t.Fatalf("previous-code pending-state reopen failed: %v\n%s", runErr, output)
		}
		db, err = store.Open(ctx, database)
		processRequire(t, err)
		if actual := readProcessSnapshot(t, db); !reflect.DeepEqual(actual, pending) {
			t.Fatal("previous-code reopen changed pending state before new-code resume")
		}
		t.Log("new code stopped; previous code reopened unchanged pending conversations, delivery and approval; resuming new code")
	}
	mu.Lock()
	restarted = true
	mu.Unlock()
	second := start("after")
	wait(func() bool {
		answer, err := db.ReadConversationAnswer(ctx, "discord", "100", "200", "process-answer")
		return err == nil && !answer.CompletedAt.IsZero()
	})
	stop(second, "after")
	t.Log("phase: resumed new-code component delivered remaining saved reply and stopped/drained")
	after, err := db.ReadConversation(ctx, "discord", "200")
	processRequire(t, err)
	if after.ID != conversation.ID || after.SessionHandle != "retained-native-session" || after.StateDir != conversation.StateDir {
		t.Fatal("restart changed native identity")
	}
	retained, _, err := db.ReadConversationCapture(ctx, after.ID)
	processRequire(t, err)
	if !reflect.DeepEqual(retained, raw) {
		t.Fatal("restart replaced captured instructions")
	}
	completed := readProcessSnapshot(t, db)
	if !reflect.DeepEqual(completed.Proposal, pending.Proposal) || !reflect.DeepEqual(completed.Unfinished, pending.Unfinished) || !reflect.DeepEqual(completed.UnfinishedTurn, pending.UnfinishedTurn) || !reflect.DeepEqual(completed.UnfinishedCapture, pending.UnfinishedCapture) {
		t.Fatal("new-code resume changed pending approval or unfinished conversation")
	}
	writeProcessSnapshot(t, directory, "process-manifest.json", completed)
	mu.Lock()
	defer mu.Unlock()
	if firstSends != 1 || secondSends < 2 || len(nonces) < 2 {
		t.Fatalf("effect observations: first=%d second=%d", firstSends, secondSends)
	}
	for _, nonce := range nonces {
		if nonce != nonces[0] {
			t.Fatal("retry changed nonce")
		}
	}
	t.Logf("effect observations: first part sends=%d; second part attempts=%d; retry nonce unchanged; pending approval and unfinished conversation unchanged", firstSends, secondSends)
}

func runDiscordProcessChild(t *testing.T) {
	t.Helper()
	base, database, directory := os.Getenv("TEST_BASE_URL"), os.Getenv("TEST_DATABASE_URL"), os.Getenv("LF_DISCORD_PROCESS_DIRECTORY")
	if !strings.HasPrefix(base, "http://127.0.0.1:") || directory == "" || filepath.Dir(database) != directory {
		t.Fatal("isolated local provider and test database required")
	}
	ctx, cancel := signal.NotifyContext(t.Context(), os.Interrupt)
	defer cancel()
	db, err := store.Open(ctx, database)
	processRequire(t, err)
	component, err := discordcomponent.New(discordcomponent.Options{
		Credentials: discordcomponent.Credentials{Settings: config.DiscordChat{Enabled: true, ApplicationID: "500", PublicKey: strings.Repeat("00", 32), OwnerID: "100", ChannelIDs: []string{"200", "201"}}, Token: secrets.NewToken("offline-process-token"), APIURL: base, Client: &http.Client{Timeout: 5 * time.Second}},
		Storage:     messagingstorage.New(db), Factory: factory.NewMessaging(db), Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Conversations: &messaging.ConversationOptions{Instructions: factorylibtest.Library(t), Bindings: []messaging.ConversationBinding{{Channel: "200", Name: "saved", Provider: "claude", Model: "offline"}, {Channel: "201", Name: "unfinished", Provider: "claude", Model: "offline"}}, StateRoot: directory},
	})
	processRequire(t, err)
	component.Serve(ctx)
	recorder := httptest.NewRecorder()
	component.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/litefactory/discord", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatal("admission remained open after drain")
	}
	processRequire(t, db.Close())
	processRequire(t, os.WriteFile(filepath.Join(directory, os.Getenv("LF_DISCORD_PROCESS_PHASE")+".drained"), []byte("joined and admission stopped"), 0600))
	_, err = fmt.Fprintln(os.Stdout, "component cancellation joined all workers before database close")
	processRequire(t, err)
}

type processSnapshot struct {
	Conversation      store.Conversation
	Capture           []byte
	Turn              store.ConversationTurn
	Answer            store.ConversationAnswer
	Parts             []store.ConversationAnswerPart
	Reply             store.ChatReply
	Proposal          store.OwnerAction
	Unfinished        store.Conversation
	UnfinishedCapture []byte
	UnfinishedTurn    store.ConversationTurn
}

func readProcessSnapshot(t *testing.T, db *store.DB) (snapshot processSnapshot) {
	t.Helper()
	ctx := t.Context()
	var err error
	snapshot.Conversation, err = db.ReadConversation(ctx, "discord", "200")
	processRequire(t, err)
	snapshot.Capture, _, err = db.ReadConversationCapture(ctx, snapshot.Conversation.ID)
	processRequire(t, err)
	snapshot.Turn, err = db.LatestConversationTurn(ctx, snapshot.Conversation.ID)
	processRequire(t, err)
	snapshot.Answer, err = db.ReadConversationAnswer(ctx, "discord", "100", "200", "process-answer")
	processRequire(t, err)
	snapshot.Parts, err = db.ReadConversationAnswerParts(ctx, "process-answer")
	processRequire(t, err)
	if len(snapshot.Parts) != 2 {
		t.Fatal("missing saved answer parts")
	}
	snapshot.Reply, err = db.ReadChatReply(ctx, "discord", snapshot.Parts[1].ReplyID)
	processRequire(t, err)
	snapshot.Proposal, err = db.ReadOwnerAction(ctx, "process-approval")
	processRequire(t, err)
	snapshot.Unfinished, err = db.ReadConversation(ctx, "discord", "201")
	processRequire(t, err)
	snapshot.UnfinishedCapture, _, err = db.ReadConversationCapture(ctx, snapshot.Unfinished.ID)
	processRequire(t, err)
	snapshot.UnfinishedTurn, err = db.LatestConversationTurn(ctx, snapshot.Unfinished.ID)
	processRequire(t, err)
	if _, err = db.ReadNaturalExecution(ctx, snapshot.Proposal.ExecutionID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("pending approval gained execution authority: %v", err)
	}
	return snapshot
}

func writeProcessSnapshot(t *testing.T, directory, name string, snapshot processSnapshot) {
	t.Helper()
	data, err := json.MarshalIndent(snapshot, "", "  ")
	processRequire(t, err)
	processRequire(t, os.WriteFile(filepath.Join(directory, name), data, 0600))
}
