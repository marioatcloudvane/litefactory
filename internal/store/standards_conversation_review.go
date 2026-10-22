package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
)

// StandardsConversationReview is a complete immutable exact review, with delivery progress.
type StandardsConversationReview struct {
	ID             string
	Transport      string
	OwnerID        string
	ChannelID      string
	Kind           string
	TargetID       string
	TargetVersion  int64
	BaseRevision   int64
	ChangeDigest   string
	ResultDigest   string
	Text           string
	Parts          []string
	PartDigests    []string
	ManifestDigest string
	RemoteIDs      []string
	Code           string
	PromptRemoteID string
	ExpiresAt      time.Time
	ConsumedAt     time.Time
	CreatedAt      time.Time
}

func standardsReviewDigest(r StandardsConversationReview) string {
	r.RemoteIDs = nil
	r.PromptRemoteID = ""
	r.ExpiresAt = time.Time{}
	r.ConsumedAt = time.Time{}
	r.CreatedAt = time.Time{}
	r.ManifestDigest = ""
	r.Code = ""
	raw, _ := json.Marshal(r)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func validateStandardsConversationReview(r StandardsConversationReview) error {
	if r.ID == "" || r.Transport == "" || r.OwnerID == "" || r.ChannelID == "" || r.Kind == "" || r.TargetID == "" || r.Code == "" || r.Text == "" || len(r.Parts) == 0 {
		return fmt.Errorf("invalid standards review manifest")
	}
	if strings.Join(r.Parts, "") != r.Text || len(r.PartDigests) != len(r.Parts) || len(r.RemoteIDs) != len(r.Parts) || r.ManifestDigest != standardsReviewDigest(r) {
		return ErrStandardsCorrupt
	}
	for i, p := range r.Parts {
		sum := sha256.Sum256([]byte(p))
		if p == "" || len(utf16.Encode([]rune(p))) > 2000 || r.PartDigests[i] != hex.EncodeToString(sum[:]) {
			return ErrStandardsCorrupt
		}
	}
	return nil
}
func readStandardsConversationReview(ctx context.Context, tx *sql.Tx, id string) (StandardsConversationReview, error) {
	var r StandardsConversationReview
	var raw string
	e := tx.QueryRowContext(ctx, `SELECT record_json FROM standards_conversation_reviews WHERE id=?`, id).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if e != nil {
		return r, e
	}
	if e = json.Unmarshal([]byte(raw), &r); e != nil {
		return r, ErrStandardsCorrupt
	}
	return r, validateStandardsConversationReview(r)
}
func writeStandardsConversationReview(ctx context.Context, tx *sql.Tx, r StandardsConversationReview) error {
	raw, e := json.Marshal(r)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE standards_conversation_reviews SET record_json=? WHERE id=?`, string(raw), r.ID)
	return e
}

// GetStandardsConversationReview reads and verifies an immutable exact review.
func (db *DB) GetStandardsConversationReview(ctx context.Context, id string) (StandardsConversationReview, error) {
	var r StandardsConversationReview
	e := db.withTx(ctx, func(tx *sql.Tx) error { var e error; r, e = readStandardsConversationReview(ctx, tx, id); return e })
	return r, e
}

// SaveStandardsConversationReview retains full exact content. Parts must concatenate to Text.
func (db *DB) SaveStandardsConversationReview(ctx context.Context, r StandardsConversationReview) (StandardsConversationReview, error) {
	r.RemoteIDs = make([]string, len(r.Parts))
	r.PartDigests = nil
	r.PromptRemoteID = ""
	r.ExpiresAt = time.Time{}
	r.ConsumedAt = time.Time{}
	r.CreatedAt = time.Now().UTC()
	for _, p := range r.Parts {
		sum := sha256.Sum256([]byte(p))
		r.PartDigests = append(r.PartDigests, hex.EncodeToString(sum[:]))
	}
	r.ManifestDigest = standardsReviewDigest(r)
	if e := validateStandardsConversationReview(r); e != nil {
		return r, e
	}
	e := db.withTx(ctx, func(tx *sql.Tx) error {
		raw, e := json.Marshal(r)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO standards_conversation_reviews(id,transport,owner_id,channel_id,code,record_json) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, r.ID, r.Transport, r.OwnerID, r.ChannelID, r.Code, string(raw))
		if e != nil {
			return e
		}
		old, e := readStandardsConversationReview(ctx, tx, r.ID)
		if e != nil {
			return e
		}
		if old.ManifestDigest != r.ManifestDigest {
			return ErrStandardsConflict
		}
		r = old
		return nil
	})
	return r, e
}
func standardsReviewScope(r StandardsConversationReview, owner, channel string) error {
	if r.OwnerID != owner || r.ChannelID != channel {
		return ErrStandardsApprovalRequired
	}
	return nil
}

// AcknowledgeStandardsConversationReviewPart records ordered transport acknowledgments.
func (db *DB) AcknowledgeStandardsConversationReviewPart(ctx context.Context, id, owner, channel string, index int, remoteID string) (StandardsConversationReview, error) {
	var r StandardsConversationReview
	if remoteID == "" {
		return r, fmt.Errorf("missing review delivery acknowledgment")
	}
	e := db.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		r, e = readStandardsConversationReview(ctx, tx, id)
		if e != nil {
			return e
		}
		if e = standardsReviewScope(r, owner, channel); e != nil {
			return e
		}
		if index < 0 || index >= len(r.Parts) {
			return fmt.Errorf("invalid review part")
		}
		for i := 0; i < index; i++ {
			if r.RemoteIDs[i] == "" {
				return ErrStandardsConflict
			}
		}
		if r.RemoteIDs[index] != "" {
			return nil
		}
		if !r.ConsumedAt.IsZero() {
			return ErrStandardsConflict
		}
		r.RemoteIDs[index] = remoteID
		return writeStandardsConversationReview(ctx, tx, r)
	})
	return r, e
}

// ActivateStandardsConversationReview starts the 15-minute gate only after all content was offered.
func (db *DB) ActivateStandardsConversationReview(ctx context.Context, id, owner, channel, promptRemoteID string, now time.Time) (StandardsConversationReview, error) {
	var r StandardsConversationReview
	if promptRemoteID == "" || now.IsZero() {
		return r, fmt.Errorf("missing final review delivery")
	}
	e := db.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		r, e = readStandardsConversationReview(ctx, tx, id)
		if e != nil {
			return e
		}
		if e = standardsReviewScope(r, owner, channel); e != nil {
			return e
		}
		if !r.ConsumedAt.IsZero() {
			return ErrStandardsConflict
		}
		for _, remote := range r.RemoteIDs {
			if remote == "" {
				return ErrStandardsApprovalRequired
			}
		}
		if r.PromptRemoteID != "" && now.Before(r.ExpiresAt) {
			return nil
		}
		r.PromptRemoteID = promptRemoteID
		r.ExpiresAt = now.UTC().Add(15 * time.Minute)
		return writeStandardsConversationReview(ctx, tx, r)
	})
	return r, e
}

// ConsumeStandardsConversationReview is a one-use gate. Domain rules must still be checked.
// WithStandardsConversationReviewConsumption allows the gate and domain effect to commit together.
func (db *DB) ConsumeStandardsConversationReview(ctx context.Context, id, owner, channel, code string, now time.Time) (StandardsConversationReview, error) {
	var r StandardsConversationReview
	e := db.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		r, e = consumeStandardsConversationReview(ctx, tx, id, owner, channel, code, now)
		return e
	})
	return r, e
}
func consumeStandardsConversationReview(ctx context.Context, tx *sql.Tx, id, owner, channel, code string, now time.Time) (StandardsConversationReview, error) {
	r, e := readStandardsConversationReview(ctx, tx, id)
	if e != nil {
		return r, e
	}
	if e = standardsReviewScope(r, owner, channel); e != nil {
		return r, e
	}
	if code != r.Code || r.PromptRemoteID == "" || now.IsZero() || !now.Before(r.ExpiresAt) || !r.ConsumedAt.IsZero() {
		return r, ErrStandardsApprovalRequired
	}
	for _, remote := range r.RemoteIDs {
		if remote == "" {
			return r, ErrStandardsApprovalRequired
		}
	}
	r.ConsumedAt = now.UTC()
	return r, writeStandardsConversationReview(ctx, tx, r)
}

type standardsReviewConsumptionKey struct{}
type standardsReviewConsumption struct {
	ID, Owner, Channel, Code string
	Now                      time.Time
}

// WithStandardsConversationReviewConsumption binds one-use approval to the domain transaction.
func WithStandardsConversationReviewConsumption(ctx context.Context, id, owner, channel, code string, now time.Time) context.Context {
	return context.WithValue(ctx, standardsReviewConsumptionKey{}, standardsReviewConsumption{id, owner, channel, code, now})
}

// FindStandardsConversationReviewByCode resolves one exact owner-scoped issued challenge.
func (db *DB) FindStandardsConversationReviewByCode(ctx context.Context, transport, owner, channel, code string) (StandardsConversationReview, error) {
	var r StandardsConversationReview
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var id string
		err := tx.QueryRowContext(ctx, `SELECT id FROM standards_conversation_reviews WHERE transport=? AND owner_id=? AND channel_id=? AND code=?`, transport, owner, channel, code).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		r, err = readStandardsConversationReview(ctx, tx, id)
		return err
	})
	return r, err
}

// ValidateStandardsConversationApproval requires a host-issued exact review gate on remote approval.
// Consumption still happens within the domain transaction, which rechecks current terms.
func (db *DB) ValidateStandardsConversationApproval(ctx context.Context, kind, id string, version int64, digest, owner, channel string) error {
	c, ok := ctx.Value(standardsReviewConsumptionKey{}).(standardsReviewConsumption)
	if !ok || c.Owner != owner || c.Channel != channel {
		return ErrStandardsApprovalRequired
	}
	r, err := db.GetStandardsConversationReview(ctx, c.ID)
	if err != nil {
		return err
	}
	if r.Kind != kind || r.TargetID != id || r.TargetVersion != version || r.ChangeDigest != digest || r.OwnerID != owner || r.ChannelID != channel || r.Code != c.Code || r.PromptRemoteID == "" || c.Now.IsZero() || !c.Now.Before(r.ExpiresAt) || !r.ConsumedAt.IsZero() {
		return ErrStandardsApprovalRequired
	}
	for _, remote := range r.RemoteIDs {
		if remote == "" {
			return ErrStandardsApprovalRequired
		}
	}
	return nil
}

// FindStandardsConversationReviewByPrompt resolves an exact retained final review prompt.
func (db *DB) FindStandardsConversationReviewByPrompt(ctx context.Context, transport, owner, channel, promptID string) (StandardsConversationReview, error) {
	var r StandardsConversationReview
	if promptID == "" {
		return r, ErrNotFound
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var id string
		err := tx.QueryRowContext(ctx, `SELECT id FROM standards_conversation_reviews WHERE transport=? AND owner_id=? AND channel_id=? AND json_extract(record_json,'$.PromptRemoteID')=?`, transport, owner, channel, promptID).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		r, err = readStandardsConversationReview(ctx, tx, id)
		return err
	})
	return r, err
}

// RefreshStandardsConversationReviewChallenge rotates an expired gate without changing reviewed content.
func (db *DB) RefreshStandardsConversationReviewChallenge(ctx context.Context, id, owner, channel, newCode string, now time.Time) (StandardsConversationReview, error) {
	var r StandardsConversationReview
	if strings.TrimSpace(newCode) == "" || now.IsZero() {
		return r, fmt.Errorf("invalid refreshed standards review challenge")
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		r, err = readStandardsConversationReview(ctx, tx, id)
		if err != nil {
			return err
		}
		if err = standardsReviewScope(r, owner, channel); err != nil {
			return err
		}
		if !r.ConsumedAt.IsZero() || r.PromptRemoteID == "" || r.ExpiresAt.IsZero() || now.Before(r.ExpiresAt) || r.Code == newCode {
			return ErrStandardsApprovalRequired
		}
		r.Code = newCode
		r.PromptRemoteID = ""
		r.ExpiresAt = time.Time{}
		raw, err := json.Marshal(r)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE standards_conversation_reviews SET code=?,record_json=? WHERE id=?`, r.Code, string(raw), r.ID)
		return err
	})
	return r, err
}
