package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/logevidence"
	"github.com/cloudvane/litefactory/internal/store"
)

// The single main-path story test enters through the local command and real
// stdio adapter, without any current task or result submission authority.
func TestMCPLogsAuthorizedPreviousRunsEndToEnd(t *testing.T) {
	paths := config.NewPaths(t.TempDir())
	t.Setenv(config.EnvHome, paths.Home())
	const secret = "known-reader-credential-49172"
	t.Setenv("LF_READER_TEST_TOKEN", secret)
	writeInitConfig(t, paths, "providers:\n  github:\n    token_env: LF_READER_TEST_TOKEN\nagents:\n  claude: {}\n")
	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(paths.Home(), "repo"), Forge: store.ForgeGitHub, Owner: "fixture", Name: "logs", RemoteURL: "git@github.com:fixture/logs.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(paths.Home(), "worktrees")})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(paths.RunsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	var selected store.Run
	var source []byte
	for _, issue := range []int{101, 202} {
		session, e := db.GetOrCreateSession(t.Context(), repo, issue)
		if e != nil {
			t.Fatal(e)
		}
		segment, e := db.GetOrCreateSegment(t.Context(), session.ID, 1, "logs", store.AgentClaude)
		if e != nil {
			t.Fatal(e)
		}
		path := filepath.Join(paths.RunsDir(), fmt.Sprintf("fixture-%d.jsonl", issue))
		run, e := db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "research", Agent: store.AgentClaude, TriggerCommentID: 1, StartedAt: time.Now().Add(-time.Minute), LogPath: func(int) string { return path }})
		if e != nil {
			t.Fatal(e)
		}
		lines := []string{`{"type":"assistant","message":{"content":[{"type":"text","text":"first known-reader-credential-49172"}]}}`, `{"type":"assistant","message":{"content":[{"type":"text","text":"second evidence"}]}}`}
		data := []byte(strings.Join(lines, "\n") + "\n")
		if e = os.WriteFile(path, data, 0o600); e != nil {
			t.Fatal(e)
		}
		if _, e = db.FinishRun(t.Context(), run.ID, store.RunOutcome{Status: store.RunSucceeded, FinishedAt: time.Now().Add(-time.Second)}); e != nil {
			t.Fatal(e)
		}
		if issue == 202 {
			selected = run
			source = data
		}
	}
	p := startMCPLogsCLI(t)
	p.handshake(t)
	p.send(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tools := p.read(t)
	if tools.Error != nil {
		t.Fatalf("tools/list: %+v", tools)
	}
	var catalog struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err = json.Unmarshal(tools.Result, &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Tools) != 2 {
		t.Fatalf("unexpected tools: %s", tools.Result)
	}
	call := func(id int, name string, args any) logevidence.Result {
		t.Helper()
		request := map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}
		raw, e := json.Marshal(request)
		if e != nil {
			t.Fatal(e)
		}
		p.send(t, string(raw))
		reply := cliStructured(t, p.read(t))
		if reply.Error != "" {
			t.Fatalf("tool error: %s", reply.Error)
		}
		if bytes.Contains(reply.Result, []byte(secret)) {
			t.Fatal("credential escaped redaction")
		}
		var result logevidence.Result
		if e = json.Unmarshal(reply.Result, &result); e != nil {
			t.Fatal(e)
		}
		return result
	}
	list := call(3, "runs_list", map[string]any{})
	if len(list.Runs) != 2 || list.Runs[0].TaskRef == list.Runs[1].TaskRef {
		t.Fatalf("cross-task discovery: %+v", list)
	}
	first := call(4, "run_logs_read", map[string]any{"run_id": selected.ID, "limit": 1})
	if len(first.Records) != 1 || first.NextCursor == "" || first.Coverage.EndOfSource || first.Records[0].Redactions == 0 {
		t.Fatalf("first page: %+v", first)
	}
	second := call(5, "run_logs_read", map[string]any{"run_id": selected.ID, "limit": 1, "cursor": first.NextCursor})
	if len(second.Records) != 1 || !second.Coverage.EndOfSource || second.NextCursor != "" || !strings.Contains(second.Records[0].Text, "second evidence") {
		t.Fatalf("continuation: %+v", second)
	}
	firstEnd := int64(bytes.IndexByte(source, '\n') + 1)
	for i, page := range []logevidence.Result{first, second} {
		record := page.Records[0]
		citation := record.Citation
		sum := sha256.Sum256([]byte(record.Text))
		expectedStart, expectedEnd := int64(0), firstEnd
		if i == 1 {
			expectedStart, expectedEnd = firstEnd, int64(len(source))
		}
		if citation.Version != 1 || citation.RunID != selected.ID || citation.Ordinal != selected.Ordinal || citation.TaskRef != "github:fixture/logs#202" || citation.Start != expectedStart || citation.End != expectedEnd || citation.Digest != hex.EncodeToString(sum[:]) || citation.Policy == "" {
			t.Fatalf("unusable citation: %+v", citation)
		}
	}
}

func startMCPLogsCLI(t *testing.T) *mcpCLIProcess {
	t.Helper()
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	p := &mcpCLIProcess{input: inW, output: outR, reader: bufio.NewReader(outR), done: make(chan struct{}), cancel: cancel}
	var command *Command
	for _, c := range Commands() {
		if c.Name == "mcp logs" {
			command = c
			break
		}
	}
	if command == nil {
		t.Fatal("mcp logs not registered")
	}
	flags := flag.NewFlagSet("mcp logs", flag.ContinueOnError)
	handler := command.New(flags)
	if err = flags.Parse([]string{"--repo", "github:fixture/logs"}); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(p.done)
		defer inR.Close()
		defer outW.Close()
		p.err = handler(ctx, &Env{Stdin: inR, Stdout: outW, Stderr: &p.stderr, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, nil)
	}()
	t.Cleanup(func() { cancel(); _ = inW.Close(); _ = outR.Close(); p.join(t) })
	return p
}
