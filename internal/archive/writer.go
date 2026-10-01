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
	"time"
)

// ErrExists reports that a final archive file is already present; archives are
// never overwritten.
var ErrExists = errors.New("archive: file already exists")

// FileMode and DirMode are the permissions of archive files and directories.
const (
	FileMode os.FileMode = 0o600
	DirMode  os.FileMode = 0o700
)

// Result describes a finished (but not yet published) file.
type Result struct {
	Header       Header
	Trailer      Trailer
	TempPath     string
	FinalPath    string
	Uncompressed int64
	Compressed   int64
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// Writer streams one archive to "<final>.partial". The zero value is not
// usable; build one with [Create].
type Writer struct {
	final    string
	temp     string
	file     *os.File
	counter  *countingWriter
	gz       *gzip.Writer
	buf      *bufio.Writer
	digester *Digester
	header   Header
	raw      int64
	closed   bool
}

// Create makes the archive directories (0700) and opens "<final>.partial"
// (0600, exclusive) with the header already written. The header's archived_at
// must equal the timestamp in the final file name.
func Create(finalPath string, h Header) (*Writer, error) {
	issue, at, err := ParseName(filepath.Base(finalPath))
	if err != nil {
		return nil, err
	}
	h.Kind, h.Format, h.Version = KindHeader, FormatName, FormatVersion
	if h.Issue != issue {
		return nil, fmt.Errorf("archive: header issue %d does not match file name %q", h.Issue, filepath.Base(finalPath))
	}
	stamp, err := time.Parse(time.RFC3339, h.ArchivedAt)
	if err != nil || !stamp.UTC().Equal(at) || stamp.UTC().Format(time.RFC3339) != h.ArchivedAt {
		return nil, fmt.Errorf("archive: header archived_at %q does not match file name %q", h.ArchivedAt, filepath.Base(finalPath))
	}
	if h.SessionIDs == nil {
		h.SessionIDs = []int64{}
	}
	if err := os.MkdirAll(filepath.Dir(finalPath), DirMode); err != nil {
		return nil, fmt.Errorf("archive: create directory: %w", err)
	}
	if _, err := os.Lstat(finalPath); err == nil {
		return nil, fmt.Errorf("%w: %s", ErrExists, finalPath)
	}
	temp := finalPath + PartialSuffix
	f, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, FileMode)
	if err != nil {
		return nil, fmt.Errorf("archive: create temporary file: %w", err)
	}
	counter := &countingWriter{w: f}
	gz := gzip.NewWriter(counter)
	w := &Writer{final: finalPath, temp: temp, file: f, counter: counter, gz: gz, buf: bufio.NewWriterSize(gz, 64<<10), digester: NewDigester(), header: h}
	line, err := json.Marshal(h)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("archive: encode header: %w", err), w.Abort())
	}
	if err := w.writeLine(line); err != nil {
		return nil, errors.Join(err, w.Abort())
	}
	return w, nil
}

func (w *Writer) writeLine(line []byte) error {
	if _, err := w.buf.Write(line); err != nil {
		return fmt.Errorf("archive: write: %w", err)
	}
	if err := w.buf.WriteByte('\n'); err != nil {
		return fmt.Errorf("archive: write: %w", err)
	}
	w.raw += int64(len(line)) + 1
	return nil
}

// WriteRow appends one row and folds it into the running digests.
func (w *Writer) WriteRow(r Row) error {
	if w.closed {
		return errors.New("archive: write to finished writer")
	}
	line, err := w.digester.Add(r)
	if err != nil {
		return err
	}
	return w.writeLine(line)
}

// Finish writes the trailer, flushes, fsyncs and closes the temporary file.
// The caller should then verify it with [Verify] and either [Writer.Publish]
// or [Writer.Abort].
func (w *Writer) Finish() (Result, error) {
	if w.closed {
		return Result{}, errors.New("archive: writer already finished")
	}
	w.closed = true
	tr := w.digester.Trailer()
	line, err := json.Marshal(tr)
	if err != nil {
		return Result{}, errors.Join(fmt.Errorf("archive: encode trailer: %w", err), w.discard())
	}
	if err := w.writeLine(line); err != nil {
		return Result{}, errors.Join(err, w.discard())
	}
	if err := w.buf.Flush(); err != nil {
		return Result{}, errors.Join(fmt.Errorf("archive: flush: %w", err), w.discard())
	}
	if err := w.gz.Close(); err != nil {
		return Result{}, errors.Join(fmt.Errorf("archive: compress: %w", err), w.discard())
	}
	if err := w.file.Sync(); err != nil {
		return Result{}, errors.Join(fmt.Errorf("archive: fsync: %w", err), w.discard())
	}
	if err := w.file.Close(); err != nil {
		w.file = nil
		return Result{}, errors.Join(fmt.Errorf("archive: close: %w", err), w.discard())
	}
	w.file = nil
	return Result{Header: w.header, Trailer: tr, TempPath: w.temp, FinalPath: w.final, Uncompressed: w.raw, Compressed: w.counter.n}, nil
}

func (w *Writer) discard() error {
	var err error
	if w.file != nil {
		err = w.file.Close()
		w.file = nil
	}
	if rmErr := os.Remove(w.temp); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
		err = errors.Join(err, rmErr)
	}
	return err
}

// Abort closes and removes the temporary file. It is safe after Finish and
// after a failed step, and is a no-op once [Writer.Publish] has succeeded.
func (w *Writer) Abort() error {
	w.closed = true
	return w.discard()
}

// Publish renames the finished temporary file to its final name without ever
// replacing an existing file, then fsyncs the directory.
func (w *Writer) Publish() error {
	if !w.closed || w.file != nil {
		return errors.New("archive: publish before finish")
	}
	if err := os.Link(w.temp, w.final); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.Join(fmt.Errorf("%w: %s", ErrExists, w.final), w.discard())
		}
		return errors.Join(fmt.Errorf("archive: publish: %w", err), w.discard())
	}
	if err := os.Remove(w.temp); err != nil {
		return errors.Join(fmt.Errorf("archive: remove temporary file: %w", err), os.Remove(w.final))
	}
	return SyncDir(filepath.Dir(w.final))
}

// SyncDir fsyncs a directory so a rename or removal is durable.
func SyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("archive: open directory: %w", err)
	}
	syncErr := d.Sync()
	closeErr := d.Close()
	if syncErr != nil {
		return fmt.Errorf("archive: fsync directory: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("archive: close directory: %w", closeErr)
	}
	return nil
}

// CleanPartials removes leftover "*.partial" files under root, which can only
// be the residue of an interrupted pass. It returns how many were removed.
func CleanPartials(root string) (int, error) {
	removed := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && path == root {
				return filepath.SkipAll
			}
			return err
		}
		if d.IsDir() || filepath.Ext(path) != PartialSuffix {
			return nil
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		removed++
		return nil
	})
	if err != nil {
		return removed, fmt.Errorf("archive: clean partial files: %w", err)
	}
	return removed, nil
}
