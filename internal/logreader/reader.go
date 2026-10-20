// Package logreader supplies trusted local log lookup and bounded source scans.
// It deliberately performs no outbound redaction or authorization.
package logreader

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// ResolvePath preserves the recorded path, with the naming fallback for old rows.
// home is the configured factory home passed by the trusted caller.
func ResolvePath(home, taskRef string, run store.Run) (string, error) {
	if run.LogPath != "" {
		return run.LogPath, nil
	}
	ref, err := naming.ParseRef(taskRef)
	if err != nil {
		return "", err
	}
	return naming.RunLogPath(home, ref, run.Ordinal)
}

// ResolveRootPath resolves the same source with a runs-root-only grant.
func ResolveRootPath(runsRoot, taskRef string, run store.Run) (string, error) {
	if run.LogPath != "" {
		return run.LogPath, nil
	}
	ref, err := naming.ParseRef(taskRef)
	if err != nil {
		return "", err
	}
	return naming.RunLogPathUnderRoot(runsRoot, ref, run.Ordinal)
}

// Stream is the local CLI's deliberately unbounded, byte-preserving line reader.
// Outbound services must use Scan instead.
func Stream(reader io.Reader, write func([]byte) error) error {
	r := bufio.NewReaderSize(reader, 64<<10)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			if e := write(line); e != nil {
				return e
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// Position is a restartable raw byte location. Discarding suppresses the rest
// of a record whose prefix exceeded the previous scan's allowance.
type Position struct {
	Offset     int64 `json:"offset"`
	Discarding bool  `json:"discarding,omitempty"`
}

// Record locates source bytes; an omission never carries partial source text.
type Record struct {
	Start    int64
	End      int64
	Data     []byte
	Omission string
}

// Page describes source progress independently of returned safe evidence.
type Page struct {
	Records []Record
	Next    Position
	EOF     bool
	// Scanned counts actual source bytes read, including lookahead beyond Next.
	// Repeated calls must charge lookahead again to bound physical read work.
	Scanned int64
}

// Scan reads at most maxScanBytes, retains at most maxRecordBytes per record,
// and makes bounded discard progress over arbitrarily large source records.
func Scan(ctx context.Context, source io.ReaderAt, pos Position, maxRecords, maxRecordBytes, maxScanBytes int) (Page, error) {
	page := Page{Next: pos}
	if pos.Offset < 0 || maxRecords < 1 || maxRecordBytes < 1 || maxScanBytes < 1 {
		return page, fmt.Errorf("invalid scan limits")
	}
	start := pos.Offset
	pending := make([]byte, 0, min(maxRecordBytes, 4096))
	discarding := pos.Discarding
	var buffer [4096]byte
	for page.Scanned < int64(maxScanBytes) && len(page.Records) < maxRecords {
		if err := ctx.Err(); err != nil {
			return page, err
		}
		amount := min(len(buffer), maxScanBytes-int(page.Scanned))
		n, readErr := source.ReadAt(buffer[:amount], page.Next.Offset)
		page.Scanned += int64(n)
		if n == 0 && readErr == nil {
			return page, io.ErrNoProgress
		}
		for _, b := range buffer[:n] {
			page.Next.Offset++
			if !discarding {
				if len(pending) < maxRecordBytes {
					pending = append(pending, b)
				} else {
					pending = nil
					discarding = true
				}
			}
			if b == '\n' {
				rec := Record{Start: start, End: page.Next.Offset}
				if discarding {
					rec.Omission = "oversized"
				} else {
					rec.Data = bytes.Clone(pending)
				}
				page.Records = append(page.Records, rec)
				pending = pending[:0]
				discarding = false
				start = page.Next.Offset
				if len(page.Records) == maxRecords {
					page.Next.Discarding = false
					return page, nil
				}
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return page, readErr
			}
			page.EOF = true
			if page.Next.Offset > start {
				reason := "incomplete"
				if discarding {
					reason = "oversized"
				}
				page.Records = append(page.Records, Record{Start: start, End: page.Next.Offset, Omission: reason})
			}
			page.Next.Discarding = false
			return page, nil
		}
	}
	// A scan ending mid-record must withhold its prefix. The next call discards
	// the remainder instead of treating it as a new JSON record.
	if page.Next.Offset > start {
		reason := "scan_limit"
		if discarding {
			reason = "oversized"
		}
		page.Records = append(page.Records, Record{Start: start, End: page.Next.Offset, Omission: reason})
		page.Next.Discarding = true
	}
	return page, nil
}
