package archive

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// Where an archive's effective date came from.
const (
	DateSourceName   = "name"
	DateSourceHeader = "header"
	DateSourceMtime  = "mtime"
)

// ReadHeader decodes only the first record of the archive at path, without
// reading the rest of the file or verifying the trailer.
func ReadHeader(path string) (Header, error) {
	//nolint:gosec // G304: the path is built by the factory under its archive root, never from user input.
	f, err := os.Open(path)
	if err != nil {
		return Header{}, fmt.Errorf("archive: open: %w", err)
	}
	defer f.Close()
	return ReadHeaderFrom(f)
}

// ReadHeaderFrom is [ReadHeader] for an already opened gzip stream.
func ReadHeaderFrom(r io.Reader) (Header, error) {
	var h Header
	gz, err := gzip.NewReader(r)
	if err != nil {
		return h, fmt.Errorf("archive: gzip: %w", err)
	}
	defer gz.Close()
	line, err := readLine(bufio.NewReaderSize(gz, 64<<10))
	if err != nil {
		return h, fmt.Errorf("archive: header: %w", err)
	}
	if err := json.Unmarshal(line, &h); err != nil {
		return h, fmt.Errorf("archive: decode header: %w", err)
	}
	if h.Kind != KindHeader {
		return Header{}, errors.New("archive: first record is not a header")
	}
	return h, nil
}

// HeaderDate is the header's archived_at as a UTC time.
func HeaderDate(h Header) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, h.ArchivedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("archive: header archived_at %q: %w", h.ArchivedAt, err)
	}
	return t.UTC(), nil
}

// DateChoice is the archive date expiry measures from, where it came from and,
// when the two readable dates disagree, a warning worth logging.
type DateChoice struct {
	Date    time.Time
	Source  string
	Warning string
}

// ChooseDate decides the archive date. Equal name and header dates use the
// name. Differing dates use the later one, with a warning. A single readable
// date is used. With neither, the file's modification time is used.
func ChooseDate(name time.Time, nameOK bool, header time.Time, headerOK bool, mtime time.Time) DateChoice {
	switch {
	case nameOK && headerOK:
		if name.Equal(header) {
			return DateChoice{Date: name.UTC(), Source: DateSourceName}
		}
		warning := fmt.Sprintf("archive name date %s and header date %s differ; using the later",
			name.UTC().Format(time.RFC3339), header.UTC().Format(time.RFC3339))
		if header.After(name) {
			return DateChoice{Date: header.UTC(), Source: DateSourceHeader, Warning: warning}
		}
		return DateChoice{Date: name.UTC(), Source: DateSourceName, Warning: warning}
	case nameOK:
		return DateChoice{Date: name.UTC(), Source: DateSourceName}
	case headerOK:
		return DateChoice{Date: header.UTC(), Source: DateSourceHeader}
	}
	return DateChoice{Date: mtime.UTC(), Source: DateSourceMtime}
}

// Expired reports whether an archive dated date is past its retention: now is
// at or after date plus days of 24 hours. A date in the future is never
// expired, and neither is any retention below one day.
func Expired(now, date time.Time, days int) bool {
	if days < 1 || date.After(now) {
		return false
	}
	return !now.Before(date.Add(time.Duration(days) * 24 * time.Hour))
}
