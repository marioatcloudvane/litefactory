package messaging

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/messagingstate"
	"slices"
	"time"

	"github.com/cloudvane/litefactory/internal/alerts"
)

// pollAlertControls has its own durable cursor and finite history budget. A
// full or unavailable conversation queue cannot prevent notification controls.
func (i *Intake) pollAlertControls(ctx context.Context, channel string, at time.Time) (failure error) {
	operation := "scan_failed"
	defer func() {
		if failure != nil {
			i.diagnostic("control_scan", channel, "", operation)
		}
	}()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if !intakeID(channel) {
		return errors.New("invalid alert control channel")
	}
	operation = "capability_check_failed"
	if err := i.Reader.CheckIntakeCapability(ctx, channel); err != nil {
		return err
	}
	operation = "store_read_failed"
	scan, exists, err := i.DB.ReadAlertControlScan(ctx, channel)
	if err != nil {
		return err
	}
	if !exists {
		operation = "read_failed"
		page, err := i.Reader.ReadIntakePage(ctx, channel, i.Owner, "")
		if err != nil {
			return err
		}
		operation = "invalid_page"
		if page.Count < 0 || page.Count > 100 || (page.HighID != "" && !intakeID(page.HighID)) {
			return errors.New("invalid alert control activation page")
		}
		boundary := page.HighID
		if boundary == "" {
			boundary = "0"
		}
		operation = "activation_failed"
		if err := i.DB.ActivateAlertControlScan(ctx, channel, boundary); err != nil {
			return err
		}
		i.diagnostic("control_scan", channel, boundary, "activated_without_replay")
		return nil
	}
	if scan.Complete {
		operation = "control_execution_failed"
		if err = i.drainAlertControlScan(ctx, channel, scan, at); err != nil {
			return err
		}
		operation = "store_read_failed"
		scan, _, err = i.DB.ReadAlertControlScan(ctx, channel)
		if err != nil {
			return err
		}
	}
	expected := scan
	var admitted []messagingstate.NaturalMessage
	complete := false
	for pageNumber := 0; pageNumber < 4; pageNumber++ {
		operation = "read_failed"
		page, err := i.Reader.ReadIntakePage(ctx, channel, i.Owner, scan.Before)
		if err != nil {
			return err
		}
		operation = "invalid_page"
		if page.Count < 0 || page.Count > 100 {
			return errors.New("invalid alert control page")
		}
		if page.Count == 0 {
			complete = true
			break
		}
		if !intakeID(page.HighID) || !intakeID(page.LowID) || compareIntakeID(page.HighID, page.LowID) < 0 || (scan.Before != "" && compareIntakeID(page.HighID, scan.Before) >= 0) {
			return errors.New("invalid alert control ordering")
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
				i.diagnostic("control_scan", channel, m.SourceID, reason)
				continue
			}
			operation = "invalid_timestamp"
			if m.CreatedAt.IsZero() || m.CreatedAt.After(at) {
				return errors.New("alert control timestamp not admissible")
			}
			if at.Sub(m.CreatedAt) > 24*time.Hour {
				i.diagnostic("control_scan", channel, m.SourceID, "expired_history")
				continue
			}
			if !alerts.Reserved(m.Text) {
				i.diagnostic("control_filter", channel, m.SourceID, "non_control_text")
				if i.OrdinaryChannels != nil && !slices.Contains(i.OrdinaryChannels, channel) {
					i.diagnostic("ordinary_route", channel, m.SourceID, "no_conversation_binding")
				}
				continue
			}
			operation = "protection_failed"
			if i.Protect == nil {
				return errors.New("alert control protection unavailable")
			}
			m.Text, err = i.Protect(ctx, m.Text)
			if err != nil {
				return err
			}
			operation = "protected_text_too_large"
			if len(m.Text) > 8192 {
				return errors.New("alert control text too large")
			}
			m.Transport = i.Transport
			admitted = append(admitted, m)
		}
		scan.Before = page.LowID
		if page.Count < 100 || compareIntakeID(page.LowID, scan.Cursor) <= 0 {
			complete = true
			break
		}
	}
	if scan.HighWater == "" {
		return nil
	}
	operation = "persistence_failed"
	if err = i.DB.StageAlertControlScan(ctx, channel, expected, scan, admitted, complete); err != nil {
		return err
	}
	if !complete {
		i.diagnostic("control_scan", channel, scan.HighWater, "partial_staged")
		return nil
	}
	i.diagnostic("control_scan", channel, scan.HighWater, "released")
	operation = "store_read_failed"
	scan, _, err = i.DB.ReadAlertControlScan(ctx, channel)
	if err != nil {
		return err
	}
	operation = "control_execution_failed"
	return i.drainAlertControlScan(ctx, channel, scan, at)
}
func (i *Intake) drainAlertControlScan(ctx context.Context, channel string, scan messagingstate.AlertControlScan, at time.Time) error {
	if !scan.Complete {
		return nil
	}
	if len(scan.Messages) == 0 {
		return i.DB.FinishAlertControlMessage(ctx, channel, "")
	}
	for _, m := range scan.Messages {
		handled, err := i.Controls(ctx, m, at)
		if err != nil {
			i.diagnostic("control_effect", channel, m.SourceID, "execution_or_reply_failed")
			return err
		}
		if !handled {
			return errors.New("reserved alert control not handled")
		}
		if err = i.DB.FinishAlertControlMessage(ctx, channel, m.SourceID); err != nil {
			i.diagnostic("control_effect", channel, m.SourceID, "finish_failed")
			return err
		}
		i.diagnostic("control_effect", channel, m.SourceID, "completed")
	}
	return nil
}
