package turn

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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

// AC-001: overlapping category selection, complete paged retrieval, and a fresh
// attempt recover the same exact book even after publication advances current.
func TestStandardsEndToEndRetainedAcrossAttempts(t *testing.T) {
	db, session, options := agentResultFixture(t, "research")
	body := strings.Repeat("Keep \"quoted\" guidance <safe> & UTF-8: 日本語.\n", 900)
	first := publishRunStandards(t, db, "initial-run-book", 0, []standards.Operation{
		{Kind: "category.add", Category: &standards.Category{ID: "go", Name: "Go", Description: "Go implementation"}},
		{Kind: "category.add", Category: &standards.Category{ID: "quality", Name: "Quality", Description: "Verification"}},
		{Kind: "standard.add", Standard: &standards.Standard{ID: "shared", Title: "Shared checks", Body: body, CategoryIDs: []string{"go", "quality"}}},
		{Kind: "standard.add", Standard: &standards.Standard{ID: "testing", Title: "Testing", Body: "Run relevant tests.\n", CategoryIDs: []string{"quality"}}},
	})
	root := strconv.FormatInt(options.Attempt.RunID, 10)
	const consumer = "story-003:implementation-engineer"
	runtime := &standards.Runtime{Store: db, RootRunID: root, ConsumerKey: consumer, AttemptID: options.Attempt.Attempt}
	status, err := runtime.Bind(t.Context(), true)
	mustProgress(t, err)
	if status.Integrity != "verified" {
		t.Fatalf("initial binding: %+v", status)
	}
	launch := func(runtime *standards.Runtime) (*standardsWire, *AttemptGuard) {
		t.Helper()
		service, err := BindAgentResultService(t.Context(), options)
		mustProgress(t, err)
		family := familyResearchOptions(t, db, service)
		tools, err := NewStandardsToolService(runtime, service.Binding())
		mustProgress(t, err)
		family.Registrations = tools.Registrations()
		client, guard, _ := familySocketFixture(t, db, family)
		wire := newStandardsWire(t, client)
		catalog := wire.rpc(t, "tools/list", map[string]any{})
		for _, name := range []string{"standards_categories_list", "standards_select", "standards_read"} {
			if !strings.Contains(string(catalog), name) {
				t.Fatalf("missing %s in %s", name, catalog)
			}
		}
		return wire, guard
	}
	wire, guard := launch(runtime)
	var categories standards.CategoryPage
	wire.call(t, "standards_categories_list", map[string]any{"page_size": 1}, &categories)
	if len(categories.Categories) != 1 || categories.NextCursor == "" || categories.Status.Binding.RevisionID != first.ID {
		t.Fatalf("first category page: %+v", categories)
	}
	var second standards.CategoryPage
	wire.call(t, "standards_categories_list", map[string]any{"page_size": 1, "cursor": categories.NextCursor}, &second)
	if len(second.Categories) != 1 || second.NextCursor != "" || categories.Categories[0].ID == second.Categories[0].ID {
		t.Fatalf("second category page: %+v", second)
	}
	var selection standards.RuntimeStatus
	selectionArgs := map[string]any{"expected_version": 0, "category_ids": []string{"quality", "go", "quality"}, "reason": "Implement Go and verify the story"}
	wire.call(t, "standards_select", selectionArgs, &selection)
	if selection.Selection == nil || selection.StandardCount != 2 || selection.CategoryCount != 2 {
		t.Fatalf("overlap was not deduplicated: %+v", selection)
	}
	var retry standards.RuntimeStatus
	wire.call(t, "standards_select", selectionArgs, &retry)
	if !reflect.DeepEqual(selection.Selection, retry.Selection) {
		t.Fatal("selection retry changed retained version")
	}
	readAll := func(wire *standardsWire, selected *store.StandardsRunSelection) map[string]string {
		t.Helper()
		bodies := map[string]string{}
		cursor := ""
		pages := 0
		for {
			var page standards.ReadPage
			wire.call(t, "standards_read", map[string]any{"selection_version": selected.Version, "selection_digest": selected.Digest, "cursor": cursor}, &page)
			pages++
			if pages > 1000 {
				t.Fatal("retrieval did not finish")
			}
			if page.Status.Binding.RevisionID != first.ID || page.SelectionDigest != selected.Digest || !utf8.ValidString(page.Body) || page.Start != len(bodies[page.StandardID]) || page.End-page.Start != len(page.Body) {
				t.Fatalf("invalid page: %+v", page)
			}
			if len(page.Manifest) != 1 || page.Manifest[0].ID != page.StandardID || page.Manifest[0].Title != page.Title || page.Manifest[0].Digest != page.BodyDigest || page.Manifest[0].Bytes != page.Total {
				t.Fatalf("page manifest does not describe exact selected body: %+v", page.Manifest)
			}
			bodies[page.StandardID] += page.Body
			if page.End == page.Total {
				hash := sha256.Sum256([]byte(bodies[page.StandardID]))
				if hex.EncodeToString(hash[:]) != page.BodyDigest {
					t.Fatalf("body digest mismatch for %s", page.StandardID)
				}
			}
			if page.Complete {
				if page.NextCursor != "" {
					t.Fatal("complete page carries cursor")
				}
				break
			}
			if page.NextCursor == "" || page.NextCursor == cursor {
				t.Fatal("incomplete page lacks progress")
			}
			cursor = page.NextCursor
		}
		if pages < 3 {
			t.Fatal("fixture did not exercise body paging")
		}
		var acknowledged standards.ReadPage
		wire.call(t, "standards_read", map[string]any{"selection_version": selected.Version, "selection_digest": selected.Digest, "complete": true}, &acknowledged)
		if acknowledged.Status.RetrievalStatus != "retrieved" {
			t.Fatalf("retrieval not acknowledged: %+v", acknowledged)
		}
		return bodies
	}
	expected := map[string]string{"shared": body, "testing": "Run relevant tests.\n"}
	if got := readAll(wire, selection.Selection); !reflect.DeepEqual(got, expected) {
		t.Fatal("first attempt changed exact selected bodies")
	}
	newer := publishRunStandards(t, db, "newer-run-book", first.ID, []standards.Operation{{Kind: "standard.replace", Standard: &standards.Standard{ID: "shared", Title: "New guidance", Body: "New published content", CategoryIDs: []string{"go", "quality"}}}})
	if newer.ID == first.ID {
		t.Fatal("publication did not advance")
	}
	mustProgress(t, guard.Revoke(t.Context(), "replaced"))
	options.Attempt = agentResultRun(t, db, session, "research", "attempt-2", 2)
	resumed := &standards.Runtime{Store: db, RootRunID: root, ConsumerKey: consumer, AttemptID: options.Attempt.Attempt}
	restored, err := resumed.Bind(t.Context(), false)
	mustProgress(t, err)
	if restored.Binding.RevisionID != first.ID || restored.Binding.Digest != first.Digest || restored.Selection == nil || restored.Selection.Version != selection.Selection.Version || restored.Selection.Digest != selection.Selection.Digest {
		t.Fatalf("resume replaced retained book or selection: %+v", restored)
	}
	resumedWire, _ := launch(resumed)
	var rediscovered standards.CategoryPage
	resumedWire.call(t, "standards_categories_list", map[string]any{}, &rediscovered)
	if rediscovered.Status.Binding.RevisionID != first.ID || !reflect.DeepEqual(rediscovered.Status.Selection, selection.Selection) {
		t.Fatalf("MCP resume lost retained identity: %+v", rediscovered)
	}
	if got := readAll(resumedWire, restored.Selection); !reflect.DeepEqual(got, expected) {
		t.Fatal("resumed attempt served newer or changed bodies")
	}
}

// Retained reads are reverified even when a private-client retry carries the
// identical request identity. This wrapper models bytes changing on disk while
// keeping all selections and observations in the real fixture database.
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
		{Kind: "standard.add", Standard: &standards.Standard{ID: "checks", Title: "Checks", Body: strings.Repeat("Exact retained guidance.\n", 900), CategoryIDs: []string{"go"}}},
	})
	retained := &standardsCorruptReadStore{DB: db}
	runtime := &standards.Runtime{Store: retained, RootRunID: strconv.FormatInt(options.Attempt.RunID, 10), ConsumerKey: "node:engineer", AttemptID: options.Attempt.Attempt}
	_, err := runtime.Bind(t.Context(), true)
	mustProgress(t, err)
	selected, err := runtime.Select(t.Context(), 0, []string{"go"}, "Relevant implementation guidance")
	mustProgress(t, err)
	service, err := BindAgentResultService(t.Context(), options)
	mustProgress(t, err)
	family := familyResearchOptions(t, db, service)
	tools, err := NewStandardsToolService(runtime, service.Binding())
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
	for _, test := range []struct {
		name string
		args map[string]any
	}{
		{"standards_categories_list", map[string]any{"root_run_id": "foreign"}},
		{"standards_select", map[string]any{"expected_version": 1, "category_ids": []string{"go"}, "reason": "Try another consumer", "consumer_key": "foreign"}},
		{"standards_read", map[string]any{"selection_version": selected.Selection.Version, "selection_digest": selected.Selection.Digest, "revision_id": 999}},
		{"standards_categories_list", map[string]any{"page_size": 101}},
	} {
		if response := call(test.name, test.args); !rejected(response) {
			t.Fatalf("forged or invalid arguments accepted: %s %+v", test.name, test.args)
		}
	}
	readArgs := map[string]any{"selection_version": selected.Selection.Version, "selection_digest": selected.Selection.Digest}
	first := call("standards_read", readArgs)
	if first.Error != "" {
		t.Fatalf("initial read failed: %s", first.Error)
	}
	var page standards.ReadPage
	mustProgress(t, json.Unmarshal(first.Result, &page))
	if page.NextCursor == "" {
		t.Fatal("fixture did not require paging")
	}
	raw, err := base64.RawURLEncoding.DecodeString(page.NextCursor)
	mustProgress(t, err)
	var cursor map[string]any
	mustProgress(t, json.Unmarshal(raw, &cursor))
	cursor["Consumer"] = "foreign"
	raw, err = json.Marshal(cursor)
	mustProgress(t, err)
	forged := map[string]any{"selection_version": selected.Selection.Version, "selection_digest": selected.Selection.Digest, "cursor": base64.RawURLEncoding.EncodeToString(raw)}
	if response := call("standards_read", forged); !rejected(response) {
		t.Fatal("foreign consumer cursor accepted")
	}
	cursor["Consumer"] = runtime.ConsumerKey
	cursor["Index"] = 1
	cursor["Offset"] = 0
	raw, err = json.Marshal(cursor)
	mustProgress(t, err)
	forged["cursor"] = base64.RawURLEncoding.EncodeToString(raw)
	response := call("standards_read", forged)
	var terminalFailure struct {
		Valid *bool `json:"valid"`
	}
	if response.Error == "" {
		mustProgress(t, json.Unmarshal(response.Result, &terminalFailure))
	}
	if response.Error == "" && (terminalFailure.Valid == nil || *terminalFailure.Valid) {
		t.Fatal("forged terminal cursor claimed complete retrieval")
	}
	retained.corrupt.Store(true)
	replay := call("standards_read", readArgs)
	var failure struct {
		Valid  *bool                   `json:"valid"`
		Status standards.RuntimeStatus `json:"status"`
		Body   *string                 `json:"body"`
	}
	mustProgress(t, json.Unmarshal(replay.Result, &failure))
	if replay.RequestID != first.RequestID || !rejected(replay) || failure.Status.Integrity != "corrupt" || failure.Body != nil || strings.Contains(string(replay.Result), "Exact retained guidance") {
		t.Fatalf("replay served cached content after corruption: %+v", replay)
	}
	retained.corrupt.Store(false)
	mustProgress(t, guard.Revoke(t.Context(), "closed"))
	for _, name := range []string{"standards_categories_list", "standards_select", "standards_read"} {
		if response := call(name, map[string]any{}); response.Error == "" {
			t.Fatalf("revoked attempt served %s", name)
		}
	}
}

func TestStandardsEncodedMetadataPaging(t *testing.T) {
	db, _, options := agentResultFixture(t, "research")
	description := strings.Repeat("\"<>&\n日本語", 350)
	operations := []standards.Operation{}
	for _, id := range []string{"a", "b", "c", "d"} {
		operations = append(operations, standards.Operation{Kind: "category.add", Category: &standards.Category{ID: id, Name: id, Description: description}})
	}
	publishRunStandards(t, db, "metadata-book", 0, operations)
	runtime := &standards.Runtime{Store: db, RootRunID: strconv.FormatInt(options.Attempt.RunID, 10), ConsumerKey: "node:engineer", AttemptID: options.Attempt.Attempt}
	_, err := runtime.Bind(t.Context(), true)
	mustProgress(t, err)
	service, err := BindAgentResultService(t.Context(), options)
	mustProgress(t, err)
	family := familyResearchOptions(t, db, service)
	tools, err := NewStandardsToolService(runtime, service.Binding())
	mustProgress(t, err)
	family.Registrations = tools.Registrations()
	client, _, _ := familySocketFixture(t, db, family)
	wire := newStandardsWire(t, client)
	cursor := ""
	seen := []string{}
	pages := 0
	for {
		var page standards.CategoryPage
		wire.call(t, "standards_categories_list", map[string]any{"page_size": 100, "cursor": cursor}, &page)
		pages++
		for _, category := range page.Categories {
			if category.Description != description {
				t.Fatal("category metadata was truncated")
			}
			seen = append(seen, category.ID)
		}
		if page.NextCursor == "" {
			break
		}
		if page.NextCursor == cursor || len(page.Categories) == 0 || pages > 10 {
			t.Fatal("category paging did not advance")
		}
		cursor = page.NextCursor
	}
	if !reflect.DeepEqual(seen, []string{"a", "b", "c", "d"}) || pages < 2 {
		t.Fatalf("encoded category paging: pages=%d ids=%v", pages, seen)
	}
}
