package cli

import (
	"bytes"
	"encoding/csv"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestResearchMeterCSVAttribution(t *testing.T) {
	t.Parallel()
	legacyHeader := "project,issue,run,attempt_id,attempt_started_at,attempt_finished_at,coding_agent,mode,invocation_id,parent_invocation_id,actual_model,model_provenance,input_uncached_tokens,cache_read_tokens,cache_write_tokens,cache_write_5m_tokens,cache_write_1h_tokens,output_tokens,reasoning_tokens,price_revision_id,estimated_usd,priced_subtotal_usd,pricing_status,collection_status,diagnostic_codes,row_kind"
	header := strings.Split(strings.TrimSpace(meterCSVHeaderLine), ",")
	wantHeader := strings.Split(legacyHeader+",execution_path,run_id,graph_instance_id,graph_attempt_id,graph_node,outcome,agent_version,source_format,source_invocation_id", ",")
	if !slices.Equal(header, wantHeader) {
		t.Fatalf("CSV schema changed: got %v, want %v", header, wantHeader)
	}
	instance, attempt := int64(71), int64(82)
	for _, tc := range []struct {
		name string
		row  store.MeterRow
		want []string
	}{
		{"graph success/retains attribution", store.MeterRow{ExecutionPath: "graph", RunID: 93, GraphInstanceID: &instance, GraphAttemptID: &attempt, GraphNode: "researcher", Outcome: "success", AgentVersion: "fixture-v1", SourceFormat: "fixture-events-v2", SourceInvocationID: "provider-1"}, []string{"graph", "93", "71", "82", "researcher", "success", "fixture-v1", "fixture-events-v2", "provider-1"}},
		{"failed launch/unknown provider retained", store.MeterRow{ExecutionPath: "graph", RunID: 94, GraphInstanceID: &instance, GraphAttemptID: &attempt, GraphNode: "researcher", Outcome: "failure"}, []string{"graph", "94", "71", "82", "researcher", "failure", "", "", ""}},
		{"legacy/no invented graph link", store.MeterRow{ExecutionPath: "legacy", RunID: 95, Outcome: "success"}, []string{"legacy", "95", "", "", "", "success", "", "", ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := researchCSVRecord(t, tc.row)
			if len(got) != 35 || !slices.Equal(got[26:], tc.want) {
				t.Fatalf("attribution: got %v, want appended %v", got, tc.want)
			}
		})
	}
}

func TestResearchMeterCSVUnknownUsage(t *testing.T) {
	t.Parallel()
	zero := int64(0)
	for _, tc := range []struct {
		name      string
		tokens    store.MeterTokens
		kind      string
		wantInput string
	}{
		{"missing measurement/blank", store.MeterTokens{}, "measurement", ""},
		{"measured zero/explicit zero", store.MeterTokens{InputUncached: &zero}, "measurement", "0"},
		{"coverage/nonadditive", store.MeterTokens{InputUncached: &zero}, "coverage", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := researchCSVRecord(t, store.MeterRow{ExecutionPath: "graph", RowKind: tc.kind, Tokens: tc.tokens, CollectionStatus: "unknown", PricingStatus: "unknown"})
			if got[12] != tc.wantInput {
				t.Fatalf("input tokens = %q, want %q", got[12], tc.wantInput)
			}
			for _, i := range []int{13, 14, 15, 16, 17, 18, 20, 21} {
				if got[i] != "" {
					t.Fatalf("unknown column %d fabricated value %q", i, got[i])
				}
			}
		})
	}
}

func TestResearchMeterCSVUntrustedAttribution(t *testing.T) {
	t.Parallel()
	got := researchCSVRecord(t, store.MeterRow{GraphNode: "=node", AgentVersion: "+version", SourceFormat: "@format", SourceInvocationID: "-provider"})
	for i, want := range map[int]string{30: "'=node", 32: "'+version", 33: "'@format", 34: "'-provider"} {
		if got[i] != want {
			t.Errorf("column %d = %q, want %q", i, got[i], want)
		}
	}
}

func researchCSVRecord(t *testing.T, row store.MeterRow) []string {
	t.Helper()
	b, err := meterCSVBytes(row)
	if err != nil {
		t.Fatal(err)
	}
	got, err := csv.NewReader(bytes.NewReader(b)).Read()
	if err != nil {
		t.Fatal(err)
	}
	return got
}
