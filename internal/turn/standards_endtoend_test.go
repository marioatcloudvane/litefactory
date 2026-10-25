package turn

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/mcpadapter"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// standardsWire drives the public MCP protocol, the private broker and the real
// retained store. Each attempt gets a fresh protocol handshake and broker lease.
type standardsWire struct {
	input  *io.PipeWriter
	frames chan []byte
	next   int
}

func newStandardsWire(t *testing.T, backend submission.Client) *standardsWire {
	t.Helper()
	input, writer := io.Pipe()
	reader, output := io.Pipe()
	ctx, cancel := context.WithCancel(t.Context())
	wire := &standardsWire{input: writer, frames: make(chan []byte, 1)}
	done, readDone := make(chan struct{}), make(chan struct{})
	// These two connection goroutines are cancelled, closed and joined by cleanup.
	go func() { defer close(done); _ = (mcpadapter.Server{}).Run(ctx, input, output, backend) }()
	go func() {
		defer close(readDone)
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 4096), mcpadapter.MaxOutputBytes+1)
		for scanner.Scan() {
			frame := append([]byte(nil), scanner.Bytes()...)
			select {
			case wire.frames <- frame:
			case <-ctx.Done():
				return
			}
		}
	}()
	t.Cleanup(func() {
		cancel()
		_ = writer.Close()
		_ = reader.Close()
		for _, joined := range []chan struct{}{done, readDone} {
			select {
			case <-joined:
			case <-time.After(10 * time.Second):
				t.Error("standards MCP stream did not stop")
			}
		}
	})
	wire.rpc(t, "initialize", map[string]any{"protocolVersion": mcpadapter.ProtocolVersion, "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "standards-test", "version": "1"}})
	notification, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	mustProgress(t, err)
	_, err = writer.Write(append(notification, '\n'))
	mustProgress(t, err)
	return wire
}

func (w *standardsWire) rpc(t *testing.T, method string, params any) json.RawMessage {
	t.Helper()
	w.next++
	request, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": w.next, "method": method, "params": params})
	mustProgress(t, err)
	_, err = w.input.Write(append(request, '\n'))
	mustProgress(t, err)
	var frame []byte
	select {
	case frame = <-w.frames:
	case <-time.After(30 * time.Second):
		t.Fatal("standards MCP response timed out")
	}
	var response struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	mustProgress(t, json.Unmarshal(frame, &response))
	if response.ID != w.next || len(response.Error) > 0 {
		t.Fatalf("MCP response: %s", frame)
	}
	return response.Result
}

func (w *standardsWire) call(t *testing.T, name string, arguments any, result any) {
	t.Helper()
	raw := w.rpc(t, "tools/call", map[string]any{"name": name, "arguments": arguments})
	if len(raw) > 32<<10 {
		t.Fatalf("encoded standards result exceeds 32KiB: %d", len(raw))
	}
	var envelope struct {
		Structured submission.Response `json:"structuredContent"`
		IsError    bool                `json:"isError"`
	}
	mustProgress(t, json.Unmarshal(raw, &envelope))
	if envelope.IsError || envelope.Structured.Error != "" {
		t.Fatalf("%s rejected: %s", name, raw)
	}
	mustProgress(t, json.Unmarshal(envelope.Structured.Result, result))
}

func publishRunStandards(t *testing.T, db *store.DB, proposalID string, base int64, operations []standards.Operation) store.StandardsRevision {
	t.Helper()
	svc := &standards.Service{Store: db, OwnerID: 71}
	proposal, err := svc.Create(t.Context(), proposalID, base, operations)
	mustProgress(t, err)
	_, err = svc.Approve(t.Context(), proposal.ProposalID, proposal.Version, proposal.ChangeDigest)
	mustProgress(t, err)
	revision, err := svc.Publish(t.Context(), proposal.ProposalID, proposal.Version, proposal.ChangeDigest)
	mustProgress(t, err)
	return revision
}

// A granted role reads exactly the standards whose applies_to names it, paged
// over the real MCP wire, and a fresh attempt reads the same retained book even
// after publication advances current.
func TestStandardsEndToEndRoleReadRetainedAcrossAttempts(t *testing.T) {
	db, session, options := agentResultFixture(t, "research")
	body := strings.Repeat("Keep \"quoted\" guidance <safe> & UTF-8: 日本語.\n", 900)
	first := publishRunStandards(t, db, "initial-run-book", 0, []standards.Operation{
		{Kind: "category.add", Category: &standards.Category{ID: "go", Name: "Go", Description: "Go implementation"}},
		{Kind: "category.add", Category: &standards.Category{ID: "quality", Name: "Quality", Description: "Verification"}},
		{Kind: "standard.add", Standard: &standards.Standard{ID: "GO-001", Rule: "MUST keep shared checks", Description: body, Evidence: "Evidence.", Scope: "All repositories.", Check: "manual", CategoryID: "go", AppliesTo: []string{"story-worker", "plan-technical"}}},
		{Kind: "standard.add", Standard: &standards.Standard{ID: "TST-001", Rule: "MUST run relevant tests", Description: "Run relevant tests.", Evidence: "Evidence.", Scope: "All repositories.", Check: "automated", CategoryID: "quality", AppliesTo: []string{"story-worker"}}},
		{Kind: "standard.add", Standard: &standards.Standard{ID: "ARC-001", Rule: "MUST respect layering", Description: "Respect layering.", Evidence: "Evidence.", Scope: "All repositories.", Check: "manual", CategoryID: "go", AppliesTo: []string{"validate-architecture"}}},
	})
	root := strconv.FormatInt(options.Attempt.RunID, 10)
	const consumer = "node:story:role:implement"
	runtime := &standards.Runtime{Store: db, RootRunID: root, ConsumerKey: consumer, AttemptID: options.Attempt.Attempt}
	launch := func(runtime *standards.Runtime) (*standardsWire, *AttemptGuard) {
		t.Helper()
		service, err := BindAgentResultService(t.Context(), options)
		mustProgress(t, err)
		family := familyResearchOptions(t, db, service)
		tools, err := NewStandardsToolService(runtime, service.Binding(), "story-worker")
		mustProgress(t, err)
		family.Registrations = tools.Registrations()
		client, guard, _ := familySocketFixture(t, db, family)
		wire := newStandardsWire(t, client)
		catalog := string(wire.rpc(t, "tools/list", map[string]any{}))
		if !strings.Contains(catalog, "standards_read") || !strings.Contains(catalog, "\"standards_categories\"") || strings.Contains(catalog, "standards_select") || strings.Contains(catalog, "standards_categories_list") {
			t.Fatalf("role catalog is not the category list and read: %s", catalog)
		}
		return wire, guard
	}
	readAll := func(wire *standardsWire) (string, []string) {
		t.Helper()
		text, cursor := "", ""
		var ids []string
		for pages := 1; ; pages++ {
			var page standards.RolePage
			wire.call(t, "standards_read", map[string]any{"cursor": cursor}, &page)
			if pages > 1000 {
				t.Fatal("retrieval did not finish")
			}
			if page.Status.Binding.RevisionID != first.ID || page.Role != "story-worker" || !utf8.ValidString(page.Text) || page.Start != len(text) || page.End-page.Start != len(page.Text) {
				t.Fatalf("invalid page: %+v", page.Status)
			}
			text += page.Text
			ids = page.StandardIDs
			if page.NextCursor == "" {
				if pages < 3 || page.End != page.Total {
					t.Fatal("fixture did not exercise complete paging")
				}
				return text, ids
			}
			if page.NextCursor == cursor {
				t.Fatal("incomplete page lacks progress")
			}
			cursor = page.NextCursor
		}
	}
	wire, guard := launch(runtime)
	text, ids := readAll(wire)
	if !reflect.DeepEqual(ids, []string{"GO-001", "TST-001"}) || !strings.Contains(text, body) || strings.Contains(text, "ARC-001") {
		t.Fatalf("role read the wrong standards: %v", ids)
	}
	newer := publishRunStandards(t, db, "newer-run-book", first.ID, []standards.Operation{{Kind: "standard.replace", Standard: &standards.Standard{ID: "GO-001", Rule: "MUST follow new guidance", Description: "New published content", Evidence: "Evidence.", Scope: "All repositories.", Check: "manual", CategoryID: "go", AppliesTo: []string{"story-worker"}}}})
	if newer.ID == first.ID {
		t.Fatal("publication did not advance")
	}
	mustProgress(t, guard.Revoke(t.Context(), "replaced"))
	options.Attempt = agentResultRun(t, db, session, "research", "attempt-2", 2)
	resumed := &standards.Runtime{Store: db, RootRunID: root, ConsumerKey: consumer, AttemptID: options.Attempt.Attempt}
	resumedWire, _ := launch(resumed)
	if again, _ := readAll(resumedWire); again != text {
		t.Fatal("resumed attempt served newer or changed standards")
	}
}

// Retained reads are reverified even when a private-client retry carries the
// identical request identity. This wrapper models bytes changing on disk while
// keeping all observations in the real fixture database.
type standardsCorruptReadStore struct {
	*store.DB
	corrupt atomic.Bool
}

func (s *standardsCorruptReadStore) GetStandardsRunBinding(ctx context.Context, root string) (store.StandardsRunBinding, error) {
	binding, err := s.DB.GetStandardsRunBinding(ctx, root)
	if s.corrupt.Load() && err == nil {
		binding.SnapshotBytes = append([]byte(nil), binding.SnapshotBytes...)
		binding.SnapshotBytes = append(binding.SnapshotBytes, '!')
	}
	return binding, err
}

func TestStandardsToolsRejectForgedScopeAndReverifyReplay(t *testing.T) {
	db, _, options := agentResultFixture(t, "research")
	publishRunStandards(t, db, "security-book", 0, []standards.Operation{
		{Kind: "category.add", Category: &standards.Category{ID: "go", Name: "Go"}},
		{Kind: "standard.add", Standard: &standards.Standard{ID: "checks", Rule: "MUST run checks", Description: strings.Repeat("Exact retained guidance.\n", 1800), Evidence: "Evidence.", Scope: "All repositories.", Check: "manual", CategoryID: "go", AppliesTo: []string{"story-worker"}}},
	})
	retained := &standardsCorruptReadStore{DB: db}
	runtime := &standards.Runtime{Store: retained, RootRunID: strconv.FormatInt(options.Attempt.RunID, 10), ConsumerKey: "node:engineer", AttemptID: options.Attempt.Attempt}
	service, err := BindAgentResultService(t.Context(), options)
	mustProgress(t, err)
	family := familyResearchOptions(t, db, service)
	tools, err := NewStandardsToolService(runtime, service.Binding(), "story-worker")
	mustProgress(t, err)
	family.Registrations = tools.Registrations()
	client, guard, _ := familySocketFixture(t, db, family)
	call := func(name string, args any) submission.Response {
		t.Helper()
		raw, err := json.Marshal(args)
		mustProgress(t, err)
		response, err := client.Call(t.Context(), name, raw)
		mustProgress(t, err)
		return response
	}
	rejected := func(response submission.Response) bool {
		t.Helper()
		if response.Error != "" {
			return true
		}
		var domain struct {
			Valid *bool `json:"valid"`
		}
		mustProgress(t, json.Unmarshal(response.Result, &domain))
		return domain.Valid != nil && !*domain.Valid
	}
	for _, args := range []map[string]any{{"role": "validate-architecture"}, {"root_run_id": "foreign"}, {"consumer_key": "foreign"}} {
		if response := call("standards_read", args); !rejected(response) {
			t.Fatalf("forged arguments accepted: %+v", args)
		}
	}
	for _, name := range []string{"standards_categories_list", "standards_select"} {
		if response := call(name, map[string]any{}); response.Error == "" {
			t.Fatalf("role was offered %s", name)
		}
	}
	first := call("standards_read", map[string]any{})
	if first.Error != "" {
		t.Fatalf("initial read failed: %s", first.Error)
	}
	var page standards.RolePage
	mustProgress(t, json.Unmarshal(first.Result, &page))
	if page.NextCursor == "" {
		t.Fatal("fixture did not require paging")
	}
	raw, err := base64.RawURLEncoding.DecodeString(page.NextCursor)
	mustProgress(t, err)
	var cursor map[string]any
	mustProgress(t, json.Unmarshal(raw, &cursor))
	for field, value := range map[string]any{"Consumer": "foreign", "Selection": "role:validate-architecture"} {
		forged := map[string]any{}
		for k, v := range cursor {
			forged[k] = v
		}
		forged[field] = value
		raw, err = json.Marshal(forged)
		mustProgress(t, err)
		if response := call("standards_read", map[string]any{"cursor": base64.RawURLEncoding.EncodeToString(raw)}); !rejected(response) {
			t.Fatalf("cursor with foreign %s accepted", field)
		}
	}
	retained.corrupt.Store(true)
	replay := call("standards_read", map[string]any{})
	var failure struct {
		Valid  *bool                   `json:"valid"`
		Status standards.RuntimeStatus `json:"status"`
		Text   *string                 `json:"text"`
	}
	mustProgress(t, json.Unmarshal(replay.Result, &failure))
	if replay.RequestID != first.RequestID || !rejected(replay) || failure.Status.Integrity != "corrupt" || failure.Text != nil || strings.Contains(string(replay.Result), "Exact retained guidance") {
		t.Fatalf("replay served cached content after corruption: %+v", replay)
	}
	retained.corrupt.Store(false)
	mustProgress(t, guard.Revoke(t.Context(), "closed"))
	if response := call("standards_read", map[string]any{}); response.Error == "" {
		t.Fatal("revoked attempt served standards_read")
	}
}
