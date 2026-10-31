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
	if o.Key == "" || o.RequestDigest == "" || o.Kind == "" || o.Transport == "" || o.OwnerID == "" || (o.ChannelID == "" && o.Transport != "mcp") || o.SourceID == "" || !json.Valid(o.RequestJSON) {
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

// StandardsProvenanceValid checks local CLI, Discord or owner-action attribution formats.
// Adapter authentication remains mandatory; this validates the retained format only.
func StandardsProvenanceValid(provenance string) bool {
	if strings.HasPrefix(provenance, "owner-action:") {
		id := strings.TrimPrefix(provenance, "owner-action:")
		if id == "" || len(id) > 128 {
			return false
		}
		for _, r := range id {
			letter := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
			digit := r >= '0' && r <= '9'
			if !letter && !digit && r != '-' && r != '_' {
				return false
			}
		}
		return true
	}
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
