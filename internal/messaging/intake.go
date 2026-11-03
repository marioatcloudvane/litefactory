package messaging

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/messagingstate"
	"log/slog"
	"slices"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/alerts"
	"github.com/cloudvane/litefactory/internal/messagingstorage"
	"github.com/cloudvane/litefactory/internal/store"
)

// IntakePage retains only authenticated owner messages; IDs also cover discarded senders.
type IntakePage struct {
	Messages      []messagingstate.NaturalMessage
	HighID, LowID string
	Count         int
}

// IntakeReader exposes only bounded message reads and capability checks.
type IntakeReader interface {
	CheckIntakeCapability(context.Context, string) error
	ReadIntakePage(context.Context, string, string, string) (IntakePage, error)
}

// Intake authenticates history and releases only completed intervals. Optional
// fixed alert controls are handled independently of conversation providers.
type Intake struct {
	Logger           *slog.Logger
	DB               messagingstorage.Port
	Reader           IntakeReader
	Owner, Transport string
	Channels         []string
	// OrdinaryChannels limits conversation intake; nil preserves all channels.
	OrdinaryChannels []string
	Protect          func(context.Context, string) (string, error)
	// Controls admits fixed owner commands outside conversation staging.
	Controls func(context.Context, messagingstate.NaturalMessage, time.Time) (bool, error)
}

// Poll collects up to four pages per allowed channel and drains completed
// fixed controls without invoking a conversation provider.
func (i *Intake) Poll(ctx context.Context, at time.Time) error {
	if i.DB == nil || i.Reader == nil || !intakeID(i.Owner) {
		return errors.New("natural intake unavailable")
	}
	var failures []error
	for _, channel := range i.Channels {
		if err := i.pollAllowedChannel(ctx, channel, at); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// PollChannel collects one allowed channel, as Poll does for each.
func (i *Intake) PollChannel(ctx context.Context, channel string, at time.Time) error {
	if i.DB == nil || i.Reader == nil || !intakeID(i.Owner) || !slices.Contains(i.Channels, channel) {
		return errors.New("natural intake unavailable")
	}
	return i.pollAllowedChannel(ctx, channel, at)
}
func (i *Intake) pollAllowedChannel(ctx context.Context, channel string, at time.Time) error {
	var controlErr error
	if i.Controls != nil {
		controlErr = i.pollAlertControls(ctx, channel, at)
	}
	if i.OrdinaryChannels != nil && !slices.Contains(i.OrdinaryChannels, channel) {
		i.diagnostic("ordinary_route", channel, "", "no_conversation_binding")
		return controlErr
	}
	return errors.Join(controlErr, i.pollChannel(ctx, channel, at))
}
func (i *Intake) pollChannel(ctx context.Context, channel string, at time.Time) (failure error) {
	operation := "scan_failed"
	defer func() {
		if failure != nil {
			i.diagnostic("ordinary_scan", channel, "", operation)
		}
	}()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if !intakeID(channel) {
		return errors.New("invalid natural intake channel")
	}
	operation = "capability_check_failed"
	if err := i.Reader.CheckIntakeCapability(ctx, channel); err != nil {
		return err
	}
	operation = "store_read_failed"
	scan, err := i.DB.ReadNaturalScan(ctx, channel)
	if errors.Is(err, store.ErrNotFound) {
		operation = "read_failed"
		page, e := i.Reader.ReadIntakePage(ctx, channel, i.Owner, "")
		if e != nil {
			return e
		}
		boundary := page.HighID
		if boundary == "" {
			boundary = "0"
		}
		operation = "activation_failed"
		if err := i.DB.ActivateNaturalScan(ctx, channel, boundary); err != nil {
			return err
		}
		i.diagnostic("ordinary_scan", channel, boundary, "activated_without_replay")
		return nil
	}
	if err != nil {
		return err
	}
	expected := scan
	var admitted []messagingstate.NaturalMessage
	complete := false
	for pageNumber := 0; pageNumber < 4; pageNumber++ {
		operation = "read_failed"
		page, e := i.Reader.ReadIntakePage(ctx, channel, i.Owner, scan.Before)
		if e != nil {
			return e
		}
		operation = "invalid_page"
		if page.Count < 0 || page.Count > 100 {
			return errors.New("invalid natural intake page")
		}
		if page.Count == 0 {
			complete = true
			break
		}
		if !intakeID(page.HighID) || !intakeID(page.LowID) || compareIntakeID(page.HighID, page.LowID) < 0 || (scan.Before != "" && compareIntakeID(page.HighID, scan.Before) >= 0) {
			return errors.New("invalid natural intake ordering")
		}
		if scan.HighWater == "" {
			scan.HighWater = page.HighID
		}
		for _, m := range page.Messages {
			if m.ChannelRef != channel || m.OwnerID != i.Owner || !intakeID(m.SourceID) || compareIntakeID(m.SourceID, page.LowID) < 0 || compareIntakeID(m.SourceID, page.HighID) > 0 || compareIntakeID(m.SourceID, scan.Cursor) <= 0 || compareIntakeID(m.SourceID, scan.HighWater) > 0 {
				reason := "outside_scan_interval"
				switch {
				case m.ChannelRef != channel:
					reason = "wrong_channel"
				case m.OwnerID != i.Owner:
					reason = "wrong_owner"
				case !intakeID(m.SourceID):
					reason = "malformed_identity"
				case compareIntakeID(m.SourceID, scan.Cursor) <= 0:
					reason = "cursor_replay"
				}
				i.diagnostic("ordinary_scan", channel, m.SourceID, reason)
				continue
			}
			operation = "invalid_timestamp"
			if m.CreatedAt.IsZero() || m.CreatedAt.After(at) {
				return errors.New("natural intake timestamp is not yet admissible")
			}
			if at.Sub(m.CreatedAt) > 24*time.Hour {
				i.diagnostic("ordinary_scan", channel, m.SourceID, "expired_history")
				continue
			}
			m.Transport = i.Transport
			operation = "protection_failed"
			if i.Protect == nil {
				return errors.New("natural intake protection unavailable")
			}
			m.Text, e = i.Protect(ctx, m.Text)
			if e != nil {
				return e
			}
			operation = "protected_text_too_large"
			if len(m.Text) > 8192 {
				return errors.New("natural intake text too large")
			}

			admitted = append(admitted, m)
		}
		scan.Before = page.LowID
		if page.Count < 100 || compareIntakeID(page.LowID, scan.Cursor) <= 0 {
			complete = true
			break
		}
	}
	if scan.HighWater == "" {
		operation = "control_cleanup_failed"
		return i.drainControls(ctx, channel, at)
	}
	controls := map[string]bool{}
	if i.Controls != nil {
		for _, m := range admitted {
			if alerts.Reserved(m.Text) {
				controls[m.SourceID] = true
			}
		}
	}
	operation = "persistence_failed"
	if err := i.DB.StageNaturalScanWithControls(ctx, expected, scan, admitted, complete, controls); err != nil {
		return err
	}
	if complete {
		i.diagnostic("ordinary_scan", channel, scan.HighWater, "released")
	} else {
		i.diagnostic("ordinary_scan", channel, scan.HighWater, "partial_staged")
	}
	operation = "control_cleanup_failed"
	return i.drainControls(ctx, channel, at)
}
func intakeID(value string) bool {
	n, err := strconv.ParseUint(value, 10, 64)
	return err == nil && n > 0 && strconv.FormatUint(n, 10) == value
}
func compareIntakeID(a, b string) int {
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func (i *Intake) drainControls(ctx context.Context, channel string, at time.Time) error {
	if i.Controls == nil {
		return nil
	}
	messages, err := i.DB.PendingNaturalAlertControls(ctx, channel)
	if err != nil {
		return err
	}
	// Dedicated alert history is the sole command executor. Ordinary history
	// may have a different cursor or incomplete interval; discard only its
	// reserved copies so no control reaches a conversation or overtakes an
	// older command still staged by the dedicated scanner.
	for _, m := range messages {
		if err = i.DB.FinishNaturalMessage(ctx, channel, m.SourceID, at); err != nil {
			return err
		}
	}

	return nil
}

// diagnostic records fixed categories without content or untrusted error strings.
func (i *Intake) diagnostic(stage, channel, source, reason string) {
	logIntake(i.Logger, i.Transport, stage, channel, source, reason)
}

func logIntake(logger *slog.Logger, transport, stage, channel, source, reason string) {
	if logger == nil {
		return
	}
	fields := []any{"transport", transport, "stage", stage, "reason", reason}
	if intakeID(channel) {
		fields = append(fields, "channel_id", channel)
	}
	if intakeID(source) {
		fields = append(fields, "source_id", source)
	}
	logger.Info("Message intake", fields...)
}
