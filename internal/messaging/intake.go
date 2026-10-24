package messaging

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// IntakePage retains only authenticated owner messages; IDs also cover discarded senders.
type IntakePage struct {
	Messages      []store.NaturalMessage
	HighID, LowID string
	Count         int
}

// IntakeReader exposes only bounded message reads and capability checks.
type IntakeReader interface {
	CheckIntakeCapability(context.Context, string) error
	ReadIntakePage(context.Context, string, string, string) (IntakePage, error)
}

// Intake does no interpretation or execution; completed intervals alone become visible.
type Intake struct {
	DB               *store.DB
	Reader           IntakeReader
	Owner, Transport string
	Channels         []string
	Protect          func(context.Context, string) (string, error)
}

// Poll collects up to four pages per allowed channel without running commands.
func (i *Intake) Poll(ctx context.Context, at time.Time) error {
	if i.DB == nil || i.Reader == nil || !intakeID(i.Owner) {
		return errors.New("natural intake unavailable")
	}
	var failures []error
	for _, channel := range i.Channels {
		if err := i.pollChannel(ctx, channel, at); err != nil {
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
	return i.pollChannel(ctx, channel, at)
}
func (i *Intake) pollChannel(ctx context.Context, channel string, at time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if !intakeID(channel) {
		return errors.New("invalid natural intake channel")
	}
	if err := i.Reader.CheckIntakeCapability(ctx, channel); err != nil {
		return err
	}
	scan, err := i.DB.ReadNaturalScan(ctx, channel)
	if errors.Is(err, store.ErrNotFound) {
		page, e := i.Reader.ReadIntakePage(ctx, channel, i.Owner, "")
		if e != nil {
			return e
		}
		boundary := page.HighID
		if boundary == "" {
			boundary = "0"
		}
		return i.DB.ActivateNaturalScan(ctx, channel, boundary)
	}
	if err != nil {
		return err
	}
	expected := scan
	var admitted []store.NaturalMessage
	complete := false
	for pageNumber := 0; pageNumber < 4; pageNumber++ {
		page, e := i.Reader.ReadIntakePage(ctx, channel, i.Owner, scan.Before)
		if e != nil {
			return e
		}
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
				continue
			}
			if m.CreatedAt.IsZero() || m.CreatedAt.After(at) {
				return errors.New("natural intake timestamp is not yet admissible")
			}
			if at.Sub(m.CreatedAt) > 24*time.Hour {
				continue
			}
			m.Transport = i.Transport
			if i.Protect == nil {
				return errors.New("natural intake protection unavailable")
			}
			m.Text, e = i.Protect(ctx, m.Text)
			if e != nil {
				return e
			}
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
		return nil
	}
	return i.DB.StageNaturalScan(ctx, expected, scan, admitted, complete)
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
