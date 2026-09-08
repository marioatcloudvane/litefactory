package schedule

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrInvalidRequest means a scheduling control did not match the supported
	// grammar. Callers must refuse it instead of treating any part as agent text.
	ErrInvalidRequest = errors.New("invalid schedule request")

	// ErrAmbiguousTime means a local civil time is skipped or repeated by a
	// timezone transition and therefore cannot identify one UTC instant.
	ErrAmbiguousTime = errors.New("ambiguous schedule time")
)

// Kind identifies the scheduling control requested by an owner.
type Kind int

// Scheduling kinds separate direct work from proposal lifecycle controls.
const (
	KindDirect Kind = iota + 1
	KindPropose
	KindAccept
	KindReject
)

// Request is the validated, factory-owned representation of one /schedule
// control. DueAt is always UTC. A dependency request retains Delay because its
// due instant begins when its prerequisites complete, not when it is received.
type Request struct {
	Kind Kind

	DueAt time.Time
	Delay time.Duration
	After []int64

	Payload    string
	ProposalID int64
}

// Parse parses the text after /schedule. receivedAt and local are inputs rather
// than ambient state so parsing stays pure and testable.
func Parse(text string, receivedAt time.Time, local *time.Location) (Request, error) {
	if local == nil {
		return Request{}, invalid("local timezone is required")
	}
	if receivedAt.IsZero() {
		return Request{}, invalid("receipt time is required")
	}

	first, rest := next(text)
	switch first {
	case "-by":
		return parseBy(rest, receivedAt)
	case "-to":
		return parseTo(rest, local)
	case "--after":
		return parseAfter(rest)
	case "--propose":
		return parseProposal(rest)
	case "--accept":
		return parseDecision(rest, KindAccept)
	case "--reject":
		return parseDecision(rest, KindReject)
	default:
		return Request{}, invalid("expected -by, -to, --after, --propose, --accept, or --reject")
	}
}

func parseBy(text string, receivedAt time.Time) (Request, error) {
	delayText, payload := next(text)
	delay, err := parseDelay(delayText)
	if err != nil {
		return Request{}, err
	}
	payload, err = validPayload(payload)
	if err != nil {
		return Request{}, err
	}
	return Request{Kind: KindDirect, DueAt: receivedAt.Add(delay).UTC(), Payload: payload}, nil
}

func parseTo(text string, local *time.Location) (Request, error) {
	dateText, rest := next(text)
	timeText, rest := next(rest)
	if dateText == "" || timeText == "" {
		return Request{}, invalid("-to requires MM/DD/YYYY HH:MM:SS and a payload")
	}

	location := local
	maybeZone, payload := next(rest)
	if isZoneToken(maybeZone) {
		var err error
		location, err = time.LoadLocation(maybeZone)
		if err != nil {
			return Request{}, invalid("timezone %q is invalid", maybeZone)
		}
	} else {
		payload = rest
	}
	payload, err := validPayload(payload)
	if err != nil {
		return Request{}, err
	}

	if !civilShape(dateText, timeText) {
		return Request{}, invalid("-to time must be MM/DD/YYYY HH:MM:SS")
	}
	wall, err := time.Parse("01/02/2006 15:04:05", dateText+" "+timeText)
	if err != nil {
		return Request{}, invalid("-to time must be MM/DD/YYYY HH:MM:SS")
	}
	instant, err := civilTime(wall, location)
	if err != nil {
		return Request{}, err
	}
	return Request{Kind: KindDirect, DueAt: instant.UTC(), Payload: payload}, nil
}

func parseAfter(text string) (Request, error) {
	idsText, rest := next(text)
	after, err := parseIDs(idsText)
	if err != nil {
		return Request{}, err
	}
	flag, rest := next(rest)
	if flag != "-by" {
		return Request{}, invalid("--after requires -by HH:MM:SS and a payload")
	}
	delayText, payload := next(rest)
	delay, err := parseDelay(delayText)
	if err != nil {
		return Request{}, err
	}
	payload, err = validPayload(payload)
	if err != nil {
		return Request{}, err
	}
	return Request{Kind: KindDirect, Delay: delay, After: after, Payload: payload}, nil
}

func parseProposal(text string) (Request, error) {
	payload, err := validPayload(text)
	if err != nil {
		return Request{}, err
	}
	return Request{Kind: KindPropose, Payload: payload}, nil
}

func parseDecision(text string, kind Kind) (Request, error) {
	idText, rest := next(text)
	if strings.TrimSpace(rest) != "" {
		return Request{}, invalid("decision accepts exactly one proposal id")
	}
	id, err := positiveID(idText)
	if err != nil {
		return Request{}, err
	}
	return Request{Kind: kind, ProposalID: id}, nil
}

func parseDelay(s string) (time.Duration, error) {
	if len(s) != len("00:00:00") || s[2] != ':' || s[5] != ':' {
		return 0, invalid("delay must be HH:MM:SS")
	}
	hours, err := strconv.Atoi(s[:2])
	if err != nil {
		return 0, invalid("delay must be HH:MM:SS")
	}
	minutes, err := strconv.Atoi(s[3:5])
	if err != nil {
		return 0, invalid("delay must be HH:MM:SS")
	}
	seconds, err := strconv.Atoi(s[6:])
	if err != nil || minutes > 59 || seconds > 59 {
		return 0, invalid("delay must be HH:MM:SS")
	}
	delay := time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute + time.Duration(seconds)*time.Second
	if delay <= 0 {
		return 0, invalid("delay must be greater than zero")
	}
	return delay, nil
}

func parseIDs(s string) ([]int64, error) {
	parts := strings.Split(s, ",")
	if len(parts) == 0 {
		return nil, invalid("at least one schedule id is required")
	}
	ids := make([]int64, 0, len(parts))
	seen := make(map[int64]struct{}, len(parts))
	for _, part := range parts {
		id, err := positiveID(part)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[id]; ok {
			return nil, invalid("schedule ids must not repeat")
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

func positiveID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, invalid("id must be a positive integer")
	}
	return id, nil
}

func validPayload(text string) (string, error) {
	payload := strings.TrimSpace(text)
	if payload == "" {
		return "", invalid("payload is required")
	}
	first, _ := next(payload)
	if strings.EqualFold(first, "/schedule") {
		return "", invalid("nested /schedule is not allowed")
	}
	return payload, nil
}

// ValidatePayload applies the payload rule shared by direct schedules and
// agent proposals. Keeping it here prevents a proposal from bypassing the
// factory grammar by nesting another scheduling control.
func ValidatePayload(text string) (string, error) { return validPayload(text) }

func isZoneToken(s string) bool {
	return s == "UTC" || (!strings.HasPrefix(s, "/") && strings.Contains(s, "/"))
}

func civilShape(dateText, timeText string) bool {
	return len(dateText) == len("00/00/0000") && dateText[2] == '/' && dateText[5] == '/' &&
		len(timeText) == len("00:00:00") && timeText[2] == ':' && timeText[5] == ':'
}

func civilTime(wall time.Time, location *time.Location) (time.Time, error) {
	instant := time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), wall.Second(), 0, location)
	if !sameWall(instant.In(location), wall) {
		return time.Time{}, fmt.Errorf("%w: local time does not exist", ErrAmbiguousTime)
	}

	// A repeated civil time has two UTC instants. Sampling offsets around the
	// requested day finds both sides of real-world DST transitions without
	// relying on time.Date's implementation-specific choice of one occurrence.
	offsets := make(map[int]struct{})
	for hour := -48; hour <= 48; hour++ {
		_, offset := instant.Add(time.Duration(hour) * time.Hour).In(location).Zone()
		offsets[offset] = struct{}{}
	}
	wallUTC := time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), wall.Second(), 0, time.UTC)
	matches := 0
	for offset := range offsets {
		candidate := wallUTC.Add(-time.Duration(offset) * time.Second)
		if sameWall(candidate.In(location), wall) {
			matches++
		}
	}
	if matches != 1 {
		return time.Time{}, fmt.Errorf("%w: local time occurs more than once", ErrAmbiguousTime)
	}
	return instant, nil
}

func sameWall(got, want time.Time) bool {
	return got.Year() == want.Year() && got.Month() == want.Month() && got.Day() == want.Day() &&
		got.Hour() == want.Hour() && got.Minute() == want.Minute() && got.Second() == want.Second()
}

func next(s string) (string, string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	i := strings.IndexAny(s, " \t\r\n")
	if i < 0 {
		return s, ""
	}
	return s[:i], strings.TrimSpace(s[i:])
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidRequest, fmt.Sprintf(format, args...))
}
