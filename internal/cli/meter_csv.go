package cli

import (
	"bytes"
	"context"
	"encoding/csv"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/cloudvane/litefactory/internal/store"
)

const meterCSVHeaderLine = "project,issue,run,attempt_id,attempt_started_at,attempt_finished_at,coding_agent,mode,invocation_id,parent_invocation_id,actual_model,model_provenance,input_uncached_tokens,cache_read_tokens,cache_write_tokens,cache_write_5m_tokens,cache_write_1h_tokens,output_tokens,reasoning_tokens,price_revision_id,estimated_usd,priced_subtotal_usd,pricing_status,collection_status,diagnostic_codes,row_kind,execution_path,run_id,graph_instance_id,graph_attempt_id,graph_node,outcome,agent_version,source_format,source_invocation_id\n"

func meterCSVText(value string) string {
	if value == "" {
		return value
	}
	first := strings.TrimLeftFunc(value, unicode.IsSpace)
	leading := value[:len(value)-len(first)]
	if strings.HasPrefix(value, "'") || strings.ContainsAny(leading, "\t\r\n") || len(first) > 0 && strings.ContainsRune("=+-@", rune(first[0])) {
		return "'" + value
	}
	return value
}
func meterCSVBytes(r store.MeterRow) ([]byte, error) {
	nullable := func(v *string) string {
		if v == nil {
			return ""
		}
		return *v
	}
	number := func(v *int64) string {
		if v == nil {
			return ""
		}
		return strconv.FormatInt(*v, 10)
	}
	codes := slices.Clone(r.Diagnostics)
	if r.ProviderReportedZeroCostRefusal {
		codes = append(codes, "provider_reported_zero_cost_refusal")
	}
	slices.Sort(codes)
	codes = slices.Compact(codes)
	fields := []string{r.Project, strconv.FormatInt(r.Issue, 10), strconv.Itoa(r.Run), r.AttemptID, r.AttemptStartedAt, r.AttemptFinishedAt, r.CodingAgent, r.Mode, r.InvocationID, r.ParentInvocationID, nullable(r.ActualModel), r.ModelProvenance, number(r.Tokens.InputUncached), number(r.Tokens.CacheRead), number(r.Tokens.CacheWrite), number(r.Tokens.CacheWrite5m), number(r.Tokens.CacheWrite1h), number(r.Tokens.Output), number(r.Tokens.Reasoning), strconv.FormatInt(r.PriceRevisionID, 10), nullable(r.EstimatedUSD), nullable(r.PricedSubtotalUSD), r.PricingStatus, r.CollectionStatus, strings.Join(codes, ";"), r.RowKind, r.ExecutionPath, strconv.FormatInt(r.RunID, 10), number(r.GraphInstanceID), number(r.GraphAttemptID), r.GraphNode, r.Outcome, r.AgentVersion, r.SourceFormat, r.SourceInvocationID}
	if r.CollectionStatus != "complete" || r.PricingStatus != "priced" {
		fields[20] = ""
	}
	if r.RowKind == "coverage" {
		for i := 12; i <= 21; i++ {
			fields[i] = ""
		}
	}
	for _, i := range []int{0, 3, 6, 7, 8, 9, 10, 11, 22, 23, 24, 25, 26, 30, 31, 32, 33, 34} {
		fields[i] = meterCSVText(fields[i])
	}
	var out bytes.Buffer
	w := csv.NewWriter(&out)
	if err := w.Write(fields); err != nil {
		return nil, err
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
func writeMeterCSV(ctx context.Context, out io.Writer, snapshot *store.MeterSnapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if n, err := io.WriteString(out, meterCSVHeaderLine); err != nil {
		return err
	} else if n != len(meterCSVHeaderLine) {
		return io.ErrShortWrite
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
			b, err := meterCSVBytes(row)
			if err != nil {
				return err
			}
			n, err := out.Write(b)
			if err != nil {
				return err
			}
			if n != len(b) {
				return io.ErrShortWrite
			}
		}
	}
}
