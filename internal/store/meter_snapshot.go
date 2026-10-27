package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// MeterFilter selects entire attempts. Time bounds compare instants, preserving
// sub-millisecond query bounds even though stored timestamps use milliseconds.
type MeterFilter struct {
	RepoID, Issue, RunID     int64
	IncludeStories           bool
	CodingAgent, Mode, Model string
	TimeFrom, TimeTo         *time.Time
}

// MeterRow is an exclusive measurement or a non-additive coverage row.
type MeterRow struct {
	ExecutionPath                   string      `json:"execution_path"`
	RunID                           int64       `json:"run_id"`
	GraphInstanceID                 *int64      `json:"graph_instance_id"`
	GraphAttemptID                  *int64      `json:"graph_attempt_id"`
	GraphNode                       string      `json:"graph_node"`
	Outcome                         string      `json:"outcome"`
	AgentVersion                    string      `json:"agent_version"`
	SourceFormat                    string      `json:"source_format"`
	SourceInvocationID              string      `json:"source_invocation_id"`
	ProviderReportedZeroCostRefusal bool        `json:"provider_reported_zero_cost_refusal,omitempty"`
	Project                         string      `json:"project"`
	Issue                           int64       `json:"issue"`
	Run                             int         `json:"run"`
	AttemptID                       string      `json:"attempt_id"`
	AttemptStartedAt                string      `json:"attempt_started_at"`
	AttemptFinishedAt               string      `json:"attempt_finished_at"`
	CodingAgent                     string      `json:"coding_agent"`
	Mode                            string      `json:"mode"`
	InvocationID                    string      `json:"invocation_id"`
	ParentInvocationID              string      `json:"parent_invocation_id"`
	ActualModel                     *string     `json:"actual_model"`
	ModelProvenance                 string      `json:"model_provenance"`
	Tokens                          MeterTokens `json:"tokens"`
	PriceRevisionID                 int64       `json:"price_revision_id"`
	MeasurementID                   int64       `json:"measurement_id"`
	MeasurementRevision             int64       `json:"measurement_revision"`
	EstimatedUSD                    *string     `json:"estimated_usd"`
	PricedSubtotalUSD               *string     `json:"priced_subtotal_usd"`
	PricingStatus                   string      `json:"pricing_status"`
	CollectionStatus                string      `json:"collection_status"`
	Diagnostics                     []string    `json:"diagnostic_codes"`
	RowKind                         string      `json:"row_kind"`
}

// MeterSnapshotOptions allows the output layer's canonical serializer to supply
// exact byte accounting without introducing a store-to-CLI dependency.
// RowBytes must be pure and must never perform database, network or writer I/O.
type MeterSnapshotOptions struct {
	RowBytes    func(MeterRow) (int, error)
	HeaderBytes int
	// MaxRows and MaxBytes may lower the default 1,000,000-row and 256 MiB
	// acquisition limits. Zero retains the existing defaults.
	MaxRows  int
	MaxBytes int
	// IncludeHistoricalRates captures only rates referenced by selected rows,
	// in the same transaction as their frozen valuations.
	IncludeHistoricalRates bool
	// AllowEmptyInterval permits an equal-bound interval resolved by a trusted
	// caller (for example today exactly at UTC midnight). Reversed bounds fail.
	AllowEmptyInterval bool
}

// MeterSnapshot pages immutable TEMP data. It never retains a connection or
// cursor across Next calls; Close is required even after cancellation.
type MeterSnapshot struct {
	db              *DB
	name            string
	cursor          int64
	mu              sync.Mutex
	closed          bool
	historicalRates []MeterHistoricalRate
	selectedIssues  []int64
}

// NewMeterSnapshot materializes at most one million rows and 256 MiB in a
// transaction limited to thirty seconds. Output starts only after it succeeds.
func (db *DB) NewMeterSnapshot(ctx context.Context, f MeterFilter, options MeterSnapshotOptions) (*MeterSnapshot, error) {
	maxRows, maxBytes := options.MaxRows, options.MaxBytes
	if maxRows == 0 {
		maxRows = 1_000_000
	}
	if maxBytes == 0 {
		maxBytes = 256 << 20
	}
	if maxRows < 1 || maxRows > 1_000_000 || maxBytes < 1 || maxBytes > 256<<20 {
		return nil, errors.New("store: invalid meter snapshot limits")
	}
	if f.TimeFrom != nil && f.TimeTo != nil && (f.TimeFrom.After(*f.TimeTo) || f.TimeFrom.Equal(*f.TimeTo) && !options.AllowEmptyInterval) {
		return nil, errors.New("store: invalid meter interval")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	s := &MeterSnapshot{db: db, name: "meter_snapshot_" + hex.EncodeToString(random[:])}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := db.withTx(bounded, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(bounded, `CREATE TEMP TABLE `+s.name+` (id INTEGER PRIMARY KEY, payload TEXT NOT NULL)`); err != nil {
			return err
		}
		if f.Issue > 0 {
			s.selectedIssues = []int64{f.Issue}
			if f.IncludeStories {
				members, err := meterSnapshotFamily(bounded, tx, f.RepoID, f.Issue, maxRows+1)
				if err != nil {
					return err
				}
				s.selectedIssues = append(s.selectedIssues, members...)
				slices.Sort(s.selectedIssues)
				s.selectedIssues = slices.Compact(s.selectedIssues)
			}
		}
		if len(s.selectedIssues) > maxRows {
			return errors.New("store: meter snapshot exceeds limits; narrow scope")
		}
		membership, err := json.Marshal(s.selectedIssues)
		if err != nil {
			return err
		}
		membershipBytes := 0
		if options.IncludeHistoricalRates {
			membershipBytes = len(membership)
		}
		if options.HeaderBytes < 0 || options.HeaderBytes > maxBytes-membershipBytes {
			return errors.New("store: invalid snapshot header size")
		}
		// Source rows are read in bounded pages within this transaction, making
		// concurrent collection invisible until the complete snapshot is captured.
		predicate, args := meterPredicates(f)
		// #nosec G202 -- predicate contains constant SQL fragments only; all selectors are bound parameters.
		query := `SELECT COALESCE(p.forge||':'||p.owner||'/'||p.name,substr(dc.task_ref,1,instr(dc.task_ref,'#')-1),'factory'),COALESCE(s.issue_number,CAST(substr(dc.task_ref,instr(dc.task_ref,'#')+1) AS INTEGER),0),COALESCE(r.ordinal,0),a.id,a.started_at,COALESCE(a.finished_at,''),a.coding_agent,COALESCE(r.mode,CASE WHEN a.authoring_session_id IS NOT NULL THEN 'standards_authoring' WHEN a.judge_attempt_id IS NOT NULL THEN 'judge' END,''),COALESCE(i.id,''),COALESCE(i.parent_id,''),m.actual_model,COALESCE(m.model_provenance,''),m.input_uncached_tokens,m.cache_read_tokens,m.cache_write_tokens,m.cache_write_5m_tokens,m.cache_write_1h_tokens,m.output_tokens,m.reasoning_tokens,a.price_revision_id,COALESCE(m.id,0),COALESCE(m.revision,0),a.collection_status,COALESCE(i.collection_status,'unknown'),COALESCE(m.collection_status,'unknown'),a.diagnostic_codes_json,COALESCE(i.diagnostic_codes_json,'[]'),COALESCE(m.diagnostic_codes_json,'[]'),a.provider_reported_zero_cost_refusal,CASE WHEN a.authoring_session_id IS NOT NULL THEN 'standards_authoring' WHEN a.presentation_owner IS NOT NULL THEN 'chat_presentation_'||po.phase WHEN a.chat_repo_id IS NOT NULL THEN 'chat_answer' WHEN a.judge_attempt_id IS NOT NULL THEN 'judge' WHEN a.doctor_attempt_id IS NOT NULL THEN 'doctor' WHEN n.id IS NULL THEN 'legacy' ELSE 'graph' END,COALESCE(r.id,0),n.instance_id,n.id,COALESCE(n.node,''),a.outcome,COALESCE(a.agent_version,''),COALESCE(a.source_format,''),COALESCE(i.source_invocation_id,'') FROM meter_attempts a LEFT JOIN chat_presentation_owners po ON po.id=a.presentation_owner LEFT JOIN graph_node_attempts n ON n.id=a.graph_node_attempt_id LEFT JOIN doctor_attempts da ON da.id=a.doctor_attempt_id LEFT JOIN doctor_conversations dc ON dc.id=da.conversation_id LEFT JOIN judge_attempts ja ON ja.id=a.judge_attempt_id LEFT JOIN judge_requests jq ON jq.id=ja.request_id LEFT JOIN runs r ON r.id=a.run_id LEFT JOIN sessions s ON s.id=COALESCE(r.session_id,jq.session_id) LEFT JOIN repos p ON p.id=po.repo_id OR p.id=a.chat_repo_id OR p.id=s.repo_id OR (a.doctor_attempt_id IS NOT NULL AND lower(substr(dc.task_ref,1,instr(dc.task_ref,'#')-1))=lower(p.forge||':'||p.owner||'/'||p.name)) LEFT JOIN meter_invocations i ON i.attempt_id=a.id LEFT JOIN meter_measurements m ON m.attempt_id=i.attempt_id AND m.invocation_id=i.id WHERE ` + predicate + ` ORDER BY p.forge,p.owner,p.name,s.issue_number,r.ordinal,a.started_at,a.id,i.id,m.actual_model LIMIT 256 OFFSET ?`
		count, total := 0, options.HeaderBytes+membershipBytes
		sourceRows, sourceBytes := 0, 0
		referenced := map[meterRateReference]bool{}
		lastFilteredInvocation := [2]string{}
		if total < 0 || total > maxBytes {
			return errors.New("store: invalid snapshot header size")
		}
		for offset := 0; ; offset += 256 {
			rows, err := tx.QueryContext(bounded, query, append(slices.Clone(args), offset)...)
			if err != nil {
				return err
			}
			page, err := scanMeterPage(rows)
			closeErr := rows.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			if len(page) == 0 {
				break
			}
			for _, row := range page {
				// Connector limits bound acquisition, including source rows later
				// excluded by a model filter. Defaults keep export accounting intact.
				if options.MaxRows != 0 {
					sourceRows++
					if sourceRows > maxRows {
						return errors.New("store: meter snapshot exceeds limits; narrow scope")
					}
				}
				if options.MaxBytes != 0 {
					encodedSource, err := json.Marshal(row)
					if err != nil {
						return err
					}
					if len(encodedSource) > maxBytes-sourceBytes {
						return errors.New("store: meter snapshot exceeds limits; narrow scope")
					}
					sourceBytes += len(encodedSource)
				}
				start, err := time.Parse(time.RFC3339Nano, row.AttemptStartedAt)
				if err != nil {
					return err
				}
				if f.TimeFrom != nil && start.Before(*f.TimeFrom) || f.TimeTo != nil && !start.Before(*f.TimeTo) {
					continue
				}
				if f.Model != "" && row.ActualModel != nil && *row.ActualModel != f.Model {
					if row.CollectionStatus == "complete" {
						continue
					}
					identity := [2]string{row.AttemptID, row.InvocationID}
					if identity == lastFilteredInvocation {
						continue
					}
					lastFilteredInvocation = identity
					row.ActualModel = nil
					row.ModelProvenance = ""
					row.Tokens = MeterTokens{}
					row.MeasurementID = 0
					row.MeasurementRevision = 0
					row.RowKind = "coverage"
					row.PricingStatus = "unknown"
					row.Diagnostics = append(row.Diagnostics, "filtered_incomplete_invocation")
				}
				if row.MeasurementID != 0 {
					if err := readMeterRowValuation(bounded, tx, &row); err != nil {
						return err
					}
				}
				if options.IncludeHistoricalRates && row.MeasurementID != 0 && row.ActualModel != nil {
					ref := meterRateReference{row.PriceRevisionID, row.CodingAgent, *row.ActualModel}
					if !referenced[ref] {
						rates, err := readMeterHistoricalRates(bounded, tx, ref)
						if err != nil {
							return err
						}
						encoded, err := json.Marshal(rates)
						if err != nil {
							return err
						}
						if len(encoded) > maxBytes-total {
							return errors.New("store: meter snapshot exceeds limits; narrow scope")
						}
						total += len(encoded)
						s.historicalRates = append(s.historicalRates, rates...)
						referenced[ref] = true
					}
				}
				encoded, err := json.Marshal(row)
				if err != nil {
					return err
				}
				size := len(encoded)
				if options.RowBytes != nil {
					size, err = options.RowBytes(row)
					if err != nil {
						return err
					}
				}
				if size < 0 || size > maxBytes || total > maxBytes-size || count >= maxRows {
					return errors.New("store: meter snapshot exceeds limits; narrow scope")
				}
				total += size
				count++
				// #nosec G202 -- table name is a private constant prefix plus crypto-random hex.
				if _, err := tx.ExecContext(bounded, `INSERT INTO `+s.name+`(payload) VALUES(?)`, string(encoded)); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("store: prepare meter snapshot: %w", err)
	}
	slices.SortFunc(s.historicalRates, compareMeterHistoricalRates)
	return s, nil
}
func scanMeterPage(rows *sql.Rows) ([]MeterRow, error) {
	var page []MeterRow
	for rows.Next() {
		var r MeterRow
		var a, i, m, ad, id, md string
		err := rows.Scan(&r.Project, &r.Issue, &r.Run, &r.AttemptID, &r.AttemptStartedAt, &r.AttemptFinishedAt, &r.CodingAgent, &r.Mode, &r.InvocationID, &r.ParentInvocationID, &r.ActualModel, &r.ModelProvenance, &r.Tokens.InputUncached, &r.Tokens.CacheRead, &r.Tokens.CacheWrite, &r.Tokens.CacheWrite5m, &r.Tokens.CacheWrite1h, &r.Tokens.Output, &r.Tokens.Reasoning, &r.PriceRevisionID, &r.MeasurementID, &r.MeasurementRevision, &a, &i, &m, &ad, &id, &md, &r.ProviderReportedZeroCostRefusal, &r.ExecutionPath, &r.RunID, &r.GraphInstanceID, &r.GraphAttemptID, &r.GraphNode, &r.Outcome, &r.AgentVersion, &r.SourceFormat, &r.SourceInvocationID)
		if err != nil {
			return nil, err
		}
		// A finished attempt's coverage is authoritative: invocation and
		// measurement statuses are interim snapshots taken while collecting.
		// An attempt that recorded no invocation observed nothing and stays
		// worst-of, so it remains unknown.
		statuses := []string{a, i, m}
		if r.AttemptFinishedAt != "" && r.InvocationID != "" {
			statuses = []string{a}
		}
		r.CollectionStatus = "complete"
		for _, status := range statuses {
			if status != "complete" {
				r.CollectionStatus = "partial"
			}
			if status == "unknown" {
				r.CollectionStatus = "unknown"
				break
			}
		}
		var finalCodes []string
		if err := json.Unmarshal([]byte(ad), &finalCodes); err != nil {
			return nil, err
		}
		for _, data := range []string{ad, id, md} {
			var codes []string
			if err := json.Unmarshal([]byte(data), &codes); err != nil {
				return nil, err
			}
			for _, code := range codes {
				// Final coverage settles discovery only; all other evidence stays.
				if a == "complete" && r.AttemptFinishedAt != "" && r.InvocationID != "" && !slices.Contains(finalCodes, "usage_source_scan_incomplete") && (code == "usage_source_scan_incomplete" || strings.HasPrefix(code, "usage_source_discovery_")) {
					continue
				}
				r.Diagnostics = append(r.Diagnostics, code)
			}
		}
		r.RowKind = "measurement"
		r.PricingStatus = "unpriced"
		if r.MeasurementID == 0 {
			r.RowKind = "coverage"
			r.PricingStatus = "unknown"
		}
		page = append(page, r)
	}
	return page, rows.Err()
}

// Next returns a bounded page and releases SQLite before returning.
func (s *MeterSnapshot) Next(ctx context.Context, limit int) ([]MeterRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("store: closed meter snapshot")
	}
	if limit < 1 || limit > 256 {
		return nil, errors.New("store: meter page size must be 1..256")
	}
	// #nosec G202 -- table name is a private constant prefix plus crypto-random hex.
	rows, err := s.db.sql.QueryContext(ctx, `SELECT id,payload FROM `+s.name+` WHERE id>? ORDER BY id LIMIT ?`, s.cursor, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []MeterRow
	cursor := s.cursor
	for rows.Next() {
		var encoded string
		if err := rows.Scan(&cursor, &encoded); err != nil {
			return nil, err
		}
		var r MeterRow
		if err := json.Unmarshal([]byte(encoded), &r); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.cursor = cursor
	return result, nil
}

// Close uses bounded cleanup independently of a cancelled output context.
func (s *MeterSnapshot) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, err := s.db.sql.ExecContext(cleanup, `DROP TABLE IF EXISTS `+s.name)
	if err == nil {
		s.closed = true
	}
	return err
}

// Bound rounding selects exactly the same discrete stored instants as the
// original continuous interval: lower >= ceil(bound), upper < ceil(bound).
func meterPredicates(f MeterFilter) (string, []any) {
	clauses := []string{"1=1"}
	var args []any
	add := func(clause string, value any) { clauses = append(clauses, clause); args = append(args, value) }
	if f.RepoID != 0 {
		add("p.id=?", f.RepoID)
	}
	if f.Issue != 0 {
		issue := "COALESCE(s.issue_number,CAST(substr(dc.task_ref,instr(dc.task_ref,'#')+1) AS INTEGER),0)"
		if f.IncludeStories {
			// Membership is persisted, repository-qualified and includes retired history.
			parent := "p.forge||':'||p.owner||'/'||p.name||'#'||?"
			clauses = append(clauses, "("+issue+"=? OR "+issue+" IN (SELECT issue_number FROM plan_story_drafts WHERE parent="+parent+" AND issue_number>0 UNION SELECT issue_number FROM managed_stories WHERE parent_task="+parent+"))")
			args = append(args, f.Issue, f.Issue, f.Issue)
		} else {
			add(issue+"=?", f.Issue)
		}
	}
	if f.RunID != 0 {
		add("r.id=?", f.RunID)
	}
	if f.CodingAgent != "" {
		add("a.coding_agent=?", f.CodingAgent)
	}
	if f.Mode != "" {
		add("COALESCE(r.mode,CASE WHEN a.authoring_session_id IS NOT NULL THEN 'standards_authoring' WHEN a.judge_attempt_id IS NOT NULL THEN 'judge' END)=?", f.Mode)
	}
	for _, bound := range []struct {
		t         *time.Time
		predicate string
	}{{f.TimeFrom, "a.started_at>=?"}, {f.TimeTo, "a.started_at<?"}} {
		if bound.t != nil {
			instant := bound.t.UTC()
			rounded := instant.Truncate(time.Millisecond)
			if rounded.Before(instant) {
				rounded = rounded.Add(time.Millisecond)
			}
			add(bound.predicate, FormatTime(rounded))
		}
	}
	return strings.Join(clauses, " AND "), args
}

func readMeterRowValuation(ctx context.Context, tx *sql.Tx, row *MeterRow) error {
	err := tx.QueryRowContext(ctx, `SELECT estimated_usd,priced_subtotal_usd,pricing_status FROM meter_valuations WHERE measurement_id=? AND measurement_revision=? AND price_revision_id=?`, row.MeasurementID, row.MeasurementRevision, row.PriceRevisionID).Scan(&row.EstimatedUSD, &row.PricedSubtotalUSD, &row.PricingStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.CollectionStatus != "complete" {
		row.EstimatedUSD = nil
	}
	return nil
}
