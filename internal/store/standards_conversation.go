package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// StandardsConversationContext retains coordination separately from expiring chat bodies.
type StandardsConversationContext struct {
	Transport     string
	OwnerID       string
	ChannelID     string
	Version       int64
	SelectedJSON  json.RawMessage
	Clarification string
	UpdatedAt     time.Time
}

// StandardsConversationOperation is an immutable admitted request and its durable result.
type StandardsConversationOperation struct {
	Key             string
	RequestDigest   string
	Kind            string
	Transport       string
	OwnerID         string
	ChannelID       string
	SourceID        string
	RequestJSON     json.RawMessage
	Started         bool
	Status          string
	EffectCommitted bool
	EffectJSON      json.RawMessage
	ResultJSON      json.RawMessage
	Error           string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ErrStandardsOperationCommitted requires receipt-first reconciliation, never replay.
var ErrStandardsOperationCommitted = errors.New("standards: operation effect already committed; recover retained result")

type standardsConversationOperationKey struct{}
type standardsConversationOperationLink struct{ Key, Digest string }

// WithStandardsConversationOperation binds a shared-domain write to an admitted operation.
// Only the terminal mutation should carry this context for multi-step generation.
func WithStandardsConversationOperation(ctx context.Context, key, digest string) context.Context {
	return context.WithValue(ctx, standardsConversationOperationKey{}, standardsConversationOperationLink{key, digest})
}

// GetStandardsConversationContext reads owner-scoped retained coordination.
func (db *DB) GetStandardsConversationContext(ctx context.Context, transport, owner, channel string) (StandardsConversationContext, error) {
	var c StandardsConversationContext
	var raw string
	err := db.sql.QueryRowContext(ctx, `SELECT record_json FROM standards_conversation_contexts WHERE transport=? AND owner_id=? AND channel_id=?`, transport, owner, channel).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	err = json.Unmarshal([]byte(raw), &c)
	return c, err
}

// SaveStandardsConversationContext uses an explicit version, never a latest-reference fallback.
func (db *DB) SaveStandardsConversationContext(ctx context.Context, c StandardsConversationContext, expected int64) (StandardsConversationContext, error) {
	if c.Transport == "" || c.OwnerID == "" || c.ChannelID == "" || expected < 0 {
		return c, fmt.Errorf("invalid standards conversation context")
	}
	c.Version = expected + 1
	c.UpdatedAt = time.Now().UTC()
	raw, err := json.Marshal(c)
	if err != nil {
		return c, err
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		var r sql.Result
		var e error
		if expected == 0 {
			r, e = tx.ExecContext(ctx, `INSERT INTO standards_conversation_contexts(transport,owner_id,channel_id,version,record_json) VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING`, c.Transport, c.OwnerID, c.ChannelID, c.Version, string(raw))
		} else {
			r, e = tx.ExecContext(ctx, `UPDATE standards_conversation_contexts SET version=?,record_json=? WHERE transport=? AND owner_id=? AND channel_id=? AND version=?`, c.Version, string(raw), c.Transport, c.OwnerID, c.ChannelID, expected)
		}
		if e != nil {
			return e
		}
		n, e := r.RowsAffected()
		if e == nil && n != 1 {
			return ErrStandardsConflict
		}
		return e
	})
	return c, err
}
func readStandardsConversationOperation(ctx context.Context, tx *sql.Tx, key string) (StandardsConversationOperation, error) {
	var o StandardsConversationOperation
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT record_json FROM standards_conversation_operations WHERE operation_key=?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return o, ErrNotFound
	}
	if err != nil {
		return o, err
	}
	err = json.Unmarshal([]byte(raw), &o)
	return o, err
}
func writeStandardsConversationOperation(ctx context.Context, tx *sql.Tx, o StandardsConversationOperation) error {
	raw, err := json.Marshal(o)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE standards_conversation_operations SET record_json=? WHERE operation_key=?`, string(raw), o.Key)
	return err
}

// GetStandardsConversationOperation reads an admitted request and its exact retained outcome.
func (db *DB) GetStandardsConversationOperation(ctx context.Context, key string) (StandardsConversationOperation, error) {
	var o StandardsConversationOperation
	err := db.withTx(ctx, func(tx *sql.Tx) error { var e error; o, e = readStandardsConversationOperation(ctx, tx, key); return e })
	return o, err
}

// AdmitStandardsConversationOperation deduplicates the complete immutable request before execution.
func (db *DB) AdmitStandardsConversationOperation(ctx context.Context, o StandardsConversationOperation) (StandardsConversationOperation, error) {
	if o.Key == "" || o.RequestDigest == "" || o.Kind == "" || o.Transport == "" || o.OwnerID == "" || o.ChannelID == "" || o.SourceID == "" || !json.Valid(o.RequestJSON) {
		return o, fmt.Errorf("invalid standards conversation operation")
	}
	o.Status = "saved"
	o.Started = false
	o.EffectCommitted = false
	o.EffectJSON = nil
	o.ResultJSON = nil
	o.Error = ""
	o.CreatedAt = time.Now().UTC()
	o.UpdatedAt = o.CreatedAt
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		raw, e := json.Marshal(o)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO standards_conversation_operations(operation_key,record_json) VALUES(?,?) ON CONFLICT DO NOTHING`, o.Key, string(raw))
		if e != nil {
			return e
		}
		retained, e := readStandardsConversationOperation(ctx, tx, o.Key)
		if e != nil {
			return e
		}
		if retained.RequestDigest != o.RequestDigest || retained.Kind != o.Kind || retained.Transport != o.Transport || retained.OwnerID != o.OwnerID || retained.ChannelID != o.ChannelID || retained.SourceID != o.SourceID || !reflect.DeepEqual(retained.RequestJSON, o.RequestJSON) {
			return ErrStandardsConflict
		}
		o = retained
		return nil
	})
	return o, err
}

// FinishStandardsConversationOperation records a factual outcome after a committed effect.
func (db *DB) FinishStandardsConversationOperation(ctx context.Context, key, digest, status string, result json.RawMessage, diagnostic string) (StandardsConversationOperation, error) {
	var o StandardsConversationOperation
	if status != "completed" && status != "failed" && status != "interrupted" && status != "conflict" {
		return o, fmt.Errorf("invalid standards operation outcome")
	}
	if len(result) > 0 && !json.Valid(result) {
		return o, fmt.Errorf("invalid standards operation result")
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		o, e = readStandardsConversationOperation(ctx, tx, key)
		if e != nil {
			return e
		}
		if o.RequestDigest != digest {
			return ErrStandardsConflict
		}
		if o.Status != "saved" {
			if o.Status != status || !reflect.DeepEqual(o.ResultJSON, result) || o.Error != diagnostic {
				return ErrStandardsConflict
			}
			return nil
		}
		if status == "completed" && !o.EffectCommitted {
			return fmt.Errorf("standards operation has no committed effect")
		}
		o.Status = status
		o.ResultJSON = result
		o.Error = diagnostic
		o.UpdatedAt = time.Now().UTC()
		return writeStandardsConversationOperation(ctx, tx, o)
	})
	return o, err
}
func standardsConversationWrite(ctx context.Context, tx *sql.Tx, fn func(*sql.Tx) error) error {
	link, ok := ctx.Value(standardsConversationOperationKey{}).(standardsConversationOperationLink)
	if !ok {
		if err := consumeStandardsReviewContext(ctx, tx); err != nil {
			return err
		}
		return fn(tx)
	}
	o, err := readStandardsConversationOperation(ctx, tx, link.Key)
	if err != nil {
		return err
	}
	if o.RequestDigest != link.Digest {
		return ErrStandardsConflict
	}
	if o.EffectCommitted {
		return ErrStandardsOperationCommitted
	}
	if o.Status != "saved" {
		return ErrStandardsConflict
	}
	if err = consumeStandardsReviewContext(ctx, tx); err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		return err
	}
	if effect, ok := ctx.Value(standardsEffectKey{}).(func() StandardsConversationEffect); ok {
		e := effect()
		if e.ID == "" {
			return nil
		}
		raw, err := json.Marshal(e)
		if err != nil {
			return err
		}
		o.EffectJSON = raw
	}
	o.EffectCommitted = true
	o.UpdatedAt = time.Now().UTC()
	return writeStandardsConversationOperation(ctx, tx, o)
}

// StandardsProvenanceValid accepts local CLI or trusted numeric Discord source attribution.
// Adapter authentication remains mandatory; this validates the retained format only.
func StandardsProvenanceValid(provenance string) bool {
	if provenance == "local-cli" {
		return true
	}
	parts := strings.Split(provenance, ":")
	if len(parts) != 4 || parts[0] != "discord-owner" {
		return false
	}
	for _, part := range parts[1:] {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}
func consumeStandardsReviewContext(ctx context.Context, tx *sql.Tx) error {
	c, ok := ctx.Value(standardsReviewConsumptionKey{}).(standardsReviewConsumption)
	if !ok {
		return nil
	}
	_, err := consumeStandardsConversationReview(ctx, tx, c.ID, c.Owner, c.Channel, c.Code, c.Now)
	return err
}

type standardsProvenanceKey struct{}

// WithStandardsProvenance supplies authenticated adapter attribution to shared exception operations.
func WithStandardsProvenance(ctx context.Context, provenance string) context.Context {
	return context.WithValue(ctx, standardsProvenanceKey{}, provenance)
}
func standardsProvenance(ctx context.Context) string {
	p, ok := ctx.Value(standardsProvenanceKey{}).(string)
	if !ok {
		return "local-cli"
	}
	return p
}

// StandardsConversationEffect links a receipt to an exact retained shared-domain result.
type StandardsConversationEffect struct {
	DomainKind string `json:"domain_kind"`
	ID         string `json:"id"`
	Version    int64  `json:"version,omitempty"`
	Revision   int64  `json:"revision,omitempty"`
}
type standardsEffectKey struct{}

func withStandardsConversationEffect(ctx context.Context, effect func() StandardsConversationEffect) context.Context {
	return context.WithValue(ctx, standardsEffectKey{}, effect)
}

// WithoutStandardsConversationOperation excludes preliminary provider-attempt bookkeeping from the final effect.
func WithoutStandardsConversationOperation(ctx context.Context) context.Context {
	return context.WithValue(ctx, standardsConversationOperationKey{}, struct{}{})
}

// ErrStandardsOperationStarted prevents replay of uncertain provider generation.
var ErrStandardsOperationStarted = errors.New("standards: generation already started; inspect retained attempt and explicitly continue")

// StartStandardsConversationGeneration durably fences uncertain generation before provider I/O.
func (db *DB) StartStandardsConversationGeneration(ctx context.Context, key, digest string) (StandardsConversationOperation, error) {
	var o StandardsConversationOperation
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		o, e = readStandardsConversationOperation(ctx, tx, key)
		if e != nil {
			return e
		}
		if o.RequestDigest != digest {
			return ErrStandardsConflict
		}
		if o.EffectCommitted {
			return ErrStandardsOperationCommitted
		}
		if o.Status != "saved" {
			return ErrStandardsConflict
		}
		if o.Started {
			return ErrStandardsOperationStarted
		}
		o.Started = true
		o.UpdatedAt = time.Now().UTC()
		return writeStandardsConversationOperation(ctx, tx, o)
	})
	return o, err
}
