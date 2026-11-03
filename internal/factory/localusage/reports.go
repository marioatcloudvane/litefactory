package localusage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/meterview"
	"github.com/cloudvane/litefactory/internal/store"
	"io"
)

// Tokens preserves unknown counters separately from measured zero.
type Tokens struct {
	InputUncached *int64 `json:"input_uncached"`
	CacheRead     *int64 `json:"cache_read"`
	CacheWrite    *int64 `json:"cache_write"`
	CacheWrite5m  *int64 `json:"cache_write_5m"`
	CacheWrite1h  *int64 `json:"cache_write_1h"`
	Output        *int64 `json:"output"`
	Reasoning     *int64 `json:"reasoning"`
}

// ExportRow contains the established exclusive-measurement export fields.
type ExportRow struct {
	ExecutionPath                   string   `json:"execution_path"`
	RunID                           int64    `json:"run_id"`
	GraphInstanceID                 *int64   `json:"graph_instance_id"`
	GraphAttemptID                  *int64   `json:"graph_attempt_id"`
	GraphNode                       string   `json:"graph_node"`
	Outcome                         string   `json:"outcome"`
	AgentVersion                    string   `json:"agent_version"`
	SourceFormat                    string   `json:"source_format"`
	SourceInvocationID              string   `json:"source_invocation_id"`
	ProviderReportedZeroCostRefusal bool     `json:"provider_reported_zero_cost_refusal,omitempty"`
	Project                         string   `json:"project"`
	Issue                           int64    `json:"issue"`
	Run                             int      `json:"run"`
	AttemptID                       string   `json:"attempt_id"`
	AttemptStartedAt                string   `json:"attempt_started_at"`
	AttemptFinishedAt               string   `json:"attempt_finished_at"`
	CodingAgent                     string   `json:"coding_agent"`
	Mode                            string   `json:"mode"`
	InvocationID                    string   `json:"invocation_id"`
	ParentInvocationID              string   `json:"parent_invocation_id"`
	ActualModel                     *string  `json:"actual_model"`
	ModelProvenance                 string   `json:"model_provenance"`
	Tokens                          Tokens   `json:"tokens"`
	PriceRevisionID                 int64    `json:"price_revision_id"`
	MeasurementID                   int64    `json:"measurement_id"`
	MeasurementRevision             int64    `json:"measurement_revision"`
	EstimatedUSD                    *string  `json:"estimated_usd"`
	PricedSubtotalUSD               *string  `json:"priced_subtotal_usd"`
	PricingStatus                   string   `json:"pricing_status"`
	CollectionStatus                string   `json:"collection_status"`
	Diagnostics                     []string `json:"diagnostic_codes"`
	RowKind                         string   `json:"row_kind"`
}

func exportRow(source store.MeterRow) ExportRow {
	return ExportRow{ExecutionPath: source.ExecutionPath, RunID: source.RunID, GraphInstanceID: source.GraphInstanceID, GraphAttemptID: source.GraphAttemptID, GraphNode: source.GraphNode, Outcome: source.Outcome, AgentVersion: source.AgentVersion, SourceFormat: source.SourceFormat, SourceInvocationID: source.SourceInvocationID, ProviderReportedZeroCostRefusal: source.ProviderReportedZeroCostRefusal, Project: source.Project, Issue: source.Issue, Run: source.Run, AttemptID: source.AttemptID, AttemptStartedAt: source.AttemptStartedAt, AttemptFinishedAt: source.AttemptFinishedAt, CodingAgent: source.CodingAgent, Mode: source.Mode, InvocationID: source.InvocationID, ParentInvocationID: source.ParentInvocationID, ActualModel: source.ActualModel, ModelProvenance: source.ModelProvenance, Tokens: Tokens(source.Tokens), PriceRevisionID: source.PriceRevisionID, MeasurementID: source.MeasurementID, MeasurementRevision: source.MeasurementRevision, EstimatedUSD: source.EstimatedUSD, PricedSubtotalUSD: source.PricedSubtotalUSD, PricingStatus: source.PricingStatus, CollectionStatus: source.CollectionStatus, Diagnostics: source.Diagnostics, RowKind: source.RowKind}
}

// Report is one fully captured local usage presentation.
type Report struct {
	JSON  json.RawMessage
	Lines []string
}

// WriteText emits the captured presentation without accessing storage.
func (r Report) WriteText(out io.Writer) error {
	for _, line := range r.Lines {
		if _, err := fmt.Fprintln(out, line); err != nil {
			return err
		}
	}
	return nil
}

// ExportEncoding separates pure byte sizing from output after snapshot acquisition.
type ExportEncoding struct {
	HeaderBytes int
	RowBytes    func(ExportRow) (int, error)
	WriteHeader func() error
	WriteRow    func(ExportRow) error
}

// Reports owns registered usage selection, snapshot acquisition and report views.
type Reports struct{ db *store.DB }

// NewReports binds usage operations to the existing local installation.
func NewReports(db *store.DB) *Reports { return &Reports{db: db} }

// Project resolves only a currently registered project identity.
func (s *Reports) Project(ctx context.Context, selector string) (int64, error) {
	return meterview.ResolveProject(ctx, s.db, selector)
}

// Summary captures one coherent summary with its existing optional details.
func (s *Reports) Summary(ctx context.Context, filter contracts.UsageFilter, details, asJSON bool) (Report, error) {
	view, err := meterview.Build(ctx, s.db, store.MeterFilter(filter), details)
	if err != nil {
		return Report{}, err
	}
	if asJSON {
		data, err := json.Marshal(view)
		return Report{JSON: data}, err
	}
	return Report{Lines: view.Lines()}, nil
}

// Export releases every database cursor before invoking any output callback.
// Byte sizing is pure and happens while capturing the bounded immutable view.
func (s *Reports) Export(ctx context.Context, filter contracts.UsageFilter, encoding ExportEncoding) (err error) {
	if encoding.RowBytes == nil || encoding.WriteHeader == nil || encoding.WriteRow == nil {
		return errors.New("meter export requires a complete encoding")
	}
	snapshot, err := s.db.NewMeterSnapshot(ctx, store.MeterFilter(filter), store.MeterSnapshotOptions{HeaderBytes: encoding.HeaderBytes, RowBytes: func(row store.MeterRow) (int, error) { return encoding.RowBytes(exportRow(row)) }})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, snapshot.Close(ctx)) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := encoding.WriteHeader(); err != nil {
		return err
	}
	for {
		page, err := snapshot.Next(ctx, 256)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			return ctx.Err()
		}
		for _, row := range page {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := encoding.WriteRow(exportRow(row)); err != nil {
				return err
			}
		}
	}
}
