package archive

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Contents is what a fully verified file declares.
type Contents struct {
	Header  Header
	Trailer Trailer
}

// Read opens the archive at path, validates the header against the file name,
// passes every row to fn (which may be nil), and verifies the trailer's counts
// and digests against what was actually read. Any deviation is an error. A
// "<name>.partial" file is checked against the name it will be published as.
func Read(path string, fn func(Row) error) (Contents, error) {
	var c Contents
	//nolint:gosec // G304: the path is built by the factory under its archive root, never from user input.
	f, err := os.Open(path)
	if err != nil {
		return c, fmt.Errorf("archive: open: %w", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return c, fmt.Errorf("archive: gzip: %w", err)
	}
	br := bufio.NewReaderSize(gz, 64<<10)

	line, err := readLine(br)
	if err != nil {
		return c, fmt.Errorf("archive: header: %w", err)
	}
	if err := json.Unmarshal(line, &c.Header); err != nil {
		return c, fmt.Errorf("archive: decode header: %w", err)
	}
	if err := checkHeader(c.Header, strings.TrimSuffix(filepath.Base(path), PartialSuffix)); err != nil {
		return c, err
	}
	digester := NewDigester()
	for {
		line, err = readLine(br)
		if err != nil {
			return c, fmt.Errorf("archive: truncated before trailer: %w", err)
		}
		var kind struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(line, &kind); err != nil {
			return c, fmt.Errorf("archive: decode record: %w", err)
		}
		if kind.Kind == KindTrailer {
			if err := json.Unmarshal(line, &c.Trailer); err != nil {
				return c, fmt.Errorf("archive: decode trailer: %w", err)
			}
			break
		}
		row, err := DecodeRow(line)
		if err != nil {
			return c, err
		}
		digester.addLine(row.Table, line)
		if fn != nil {
			if err := fn(row); err != nil {
				return c, err
			}
		}
	}
	if _, err := br.ReadByte(); !errors.Is(err, io.EOF) {
		return c, errors.New("archive: data after trailer")
	}
	want := digester.Trailer()
	if !trailersEqual(want, c.Trailer) {
		return c, errors.New("archive: trailer does not match the rows in the file")
	}
	return c, nil
}

// Verify reads the whole file and checks it is complete and self-consistent.
func Verify(path string) (Contents, error) { return Read(path, nil) }

func readLine(br *bufio.Reader) ([]byte, error) {
	line, err := br.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	return line[:len(line)-1], nil
}

func checkHeader(h Header, name string) error {
	issue, at, err := ParseName(name)
	if err != nil {
		return err
	}
	if h.Kind != KindHeader || h.Format != FormatName || h.Version != FormatVersion {
		return fmt.Errorf("archive: unsupported header %q/%q/%d", h.Kind, h.Format, h.Version)
	}
	stamp, err := time.Parse(time.RFC3339, h.ArchivedAt)
	if err != nil || !stamp.UTC().Equal(at) {
		return fmt.Errorf("archive: header archived_at %q does not match file name", h.ArchivedAt)
	}
	if h.Issue != issue {
		return fmt.Errorf("archive: header issue %d does not match file name", h.Issue)
	}
	return nil
}

func trailersEqual(a, b Trailer) bool {
	if a.Digest != b.Digest || len(a.Counts) != len(b.Counts) || len(a.Digests) != len(b.Digests) {
		return false
	}
	for k, v := range a.Counts {
		if bv, ok := b.Counts[k]; !ok || bv != v {
			return false
		}
	}
	for k, v := range a.Digests {
		if bv, ok := b.Digests[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// TaskFiles lists the published archive files of one task directory, oldest
// first by name. A missing directory yields none.
func TaskFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("archive: list %s: %w", dir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if _, _, err := ParseName(e.Name()); err == nil {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out, nil
}
