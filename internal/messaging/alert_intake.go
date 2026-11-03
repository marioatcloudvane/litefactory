package messaging

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/messagingstate"
	"time"

	"github.com/cloudvane/litefactory/internal/alerts"
)

// pollAlertControls has its own durable cursor and finite history budget. A
// full or unavailable conversation queue cannot prevent notification controls.
func (i *Intake) pollAlertControls(ctx context.Context, channel string, at time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if !intakeID(channel) {
		return errors.New("invalid alert control channel")
	}
	if err := i.Reader.CheckIntakeCapability(ctx, channel); err != nil {
		return err
	}
	scan, exists, err := i.DB.ReadAlertControlScan(ctx, channel)
	if err != nil {
		return err
	}
	if !exists {
		page, err := i.Reader.ReadIntakePage(ctx, channel, i.Owner, "")
		if err != nil {
			return err
		}
		if page.Count < 0 || page.Count > 100 || (page.HighID != "" && !intakeID(page.HighID)) {
			return errors.New("invalid alert control activation page")
		}
		boundary := page.HighID
		if boundary == "" {
			boundary = "0"
		}
		return i.DB.ActivateAlertControlScan(ctx, channel, boundary)
	}
	if scan.Complete {
		if err = i.drainAlertControlScan(ctx, channel, scan, at); err != nil {
			return err
		}
		scan, _, err = i.DB.ReadAlertControlScan(ctx, channel)
		if err != nil {
			return err
		}
	}
	expected := scan
	var admitted []messagingstate.NaturalMessage
	complete := false
	for pageNumber := 0; pageNumber < 4; pageNumber++ {
		page, err := i.Reader.ReadIntakePage(ctx, channel, i.Owner, scan.Before)
		if err != nil {
			return err
		}
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
				continue
			}
			if m.CreatedAt.IsZero() || m.CreatedAt.After(at) {
				return errors.New("alert control timestamp not admissible")
			}
			if at.Sub(m.CreatedAt) > 24*time.Hour || !alerts.Reserved(m.Text) {
				continue
			}
			if i.Protect == nil {
				return errors.New("alert control protection unavailable")
			}
			m.Text, err = i.Protect(ctx, m.Text)
			if err != nil {
				return err
			}
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
	if err = i.DB.StageAlertControlScan(ctx, channel, expected, scan, admitted, complete); err != nil {
		return err
	}
	if !complete {
		return nil
	}
	scan, _, err = i.DB.ReadAlertControlScan(ctx, channel)
	if err != nil {
		return err
	}
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
			return err
		}
		if !handled {
			return errors.New("reserved alert control not handled")
		}
		if err = i.DB.FinishAlertControlMessage(ctx, channel, m.SourceID); err != nil {
			return err
		}
	}
	return nil
}
