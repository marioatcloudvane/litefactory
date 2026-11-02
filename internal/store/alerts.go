package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// AlertLedger is the alert-owned durable journal. It is independent of notice
// retention and contains no provider output or credential values.
type AlertLedger struct {
	ControlScans  map[string]AlertControlScan
	Configuration AlertConfiguration
	Revision      int64
	Subjects      map[string]*AlertSubject
	Evidence      map[string]AlertEvidence
	Notifications map[string]*AlertNotification
	Controls      AlertControls
	Receipts      map[string]AlertCommandReceipt
	Observers     map[string]AlertObserver
	Delivery      AlertObserver
}

// AlertSubject retains the current rule and subject aggregate.
type AlertSubject struct {
	Rule, Kind, Key, TaskRef, Summary, Action                             string
	Episode                                                               int64
	Severity                                                              string
	Active                                                                bool
	FirstObserved, LastObserved, LastProgress, RecoveredAt, CooldownUntil time.Time
	FailureEvidence, RecoveryEvidence                                     string
	CleanCount                                                            int
	Episodes                                                              map[int64]*AlertEpisode
	ProviderClass                                                         string
	Consecutive                                                           int
	WindowStart                                                           time.Time
}

// AlertEpisode retains confirmation debt independently of recurrence.
type AlertEpisode struct {
	Number                           int64
	Severity                         string
	OpenedAt, RecoveredAt            time.Time
	OpeningConfirmed, ClearConfirmed bool
}

// AlertEvidence retains typed source identity and recovery provenance.
type AlertEvidence struct {
	SourceSessionID, SourceTriggerID, SourceRunID                 int64
	SourceDeliveryKey                                             string
	ID, Rule, Subject, Source, Class, Condition, RecoveryEvidence string
	At                                                            time.Time
	Approximate                                                   bool
}

// AlertNotification journals immutable alert intent and fenced transport attempts.
type AlertNotification struct {
	ID, Subject, Rule, Kind, Severity, Destination, Body, Marker, Nonce string
	Episode                                                             int64
	State                                                               string
	OwnerID                                                             string
	MentionOwner                                                        bool
	Members                                                             []AlertNotificationMember
	Attempt, Fence                                                      int64
	CreatedAt, RetryAt, AttemptedAt, LeaseUntil, ConfirmedAt            time.Time
	RemoteID, LastError                                                 string
}

// AlertNotificationMember binds one transition to a digest part.
type AlertNotificationMember struct {
	Subject  string
	Episode  int64
	Kind     string
	Resolved bool
}

// AlertControls retains timed notification pauses.
type AlertControls struct {
	OffUntil  time.Time
	RuleMutes map[string]time.Time
}

// AlertCommandReceipt makes one authenticated source command replay-safe.
type AlertCommandReceipt struct {
	Source, Command, Reply string
	At, Expiry             time.Time
}

// AlertObserver distinguishes coverage and delivery availability from incident health.
type AlertObserver struct {
	Ready                    bool
	LastSuccess, LastAttempt time.Time
	Reason                   string
}

func emptyAlertLedger() AlertLedger {
	return AlertLedger{ControlScans: map[string]AlertControlScan{}, Subjects: map[string]*AlertSubject{}, Evidence: map[string]AlertEvidence{}, Notifications: map[string]*AlertNotification{}, Controls: AlertControls{RuleMutes: map[string]time.Time{}}, Receipts: map[string]AlertCommandReceipt{}, Observers: map[string]AlertObserver{}}
}
func decodeAlertLedger(raw []byte, revision int64) (AlertLedger, error) {
	state := emptyAlertLedger()
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &state); err != nil {
			return state, fmt.Errorf("decode alert ledger: %w", err)
		}
	}
	state.Revision = revision
	return state, nil
}

// ReadAlerts returns an independent snapshot; it performs no observation or repair.
func (db *DB) ReadAlerts(ctx context.Context) (AlertLedger, error) {
	var raw []byte
	var revision int64
	err := db.sql.QueryRowContext(ctx, `SELECT revision, state FROM alert_ledger WHERE id=1`).Scan(&revision, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return emptyAlertLedger(), nil
	}
	if err != nil {
		return AlertLedger{}, err
	}
	return decodeAlertLedger(raw, revision)
}

// UpdateAlerts serializes a local policy/control mutation with its journal.
// The callback must perform no network I/O or nested database access. A failed
// callback rolls back everything, including command receipts and send claims.
func (db *DB) UpdateAlerts(ctx context.Context, mutate func(*AlertLedger) error) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var raw []byte
		var revision int64
		err := tx.QueryRowContext(ctx, `SELECT revision,state FROM alert_ledger WHERE id=1`).Scan(&revision, &raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		state, err := decodeAlertLedger(raw, revision)
		if err != nil {
			return err
		}
		if err = mutate(&state); err != nil {
			return err
		}
		state.Revision = revision + 1
		data, err := json.Marshal(state)
		if err != nil {
			return err
		}
		// Fail closed before a growing ledger can exhaust memory on later reads.
		// Unresolved episodes and uncertain sends are never silently discarded.
		if len(data) > 16<<20 {
			return fmt.Errorf("alert journal capacity reached; observation unavailable")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO alert_ledger(id,revision,state) VALUES(1,?,?) ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,state=excluded.state`, state.Revision, string(data))
		return err
	})
}

// AlertConfiguration retains the effective nonsecret switches for read-only status.
type AlertConfiguration struct {
	Configured           bool
	Enabled              bool
	Rules                map[string]bool
	QuietEnabled         bool
	QuietStart, QuietEnd string
}
