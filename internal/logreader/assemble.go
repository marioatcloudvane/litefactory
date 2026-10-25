package logreader

import (
	"bytes"
	"context"
	"errors"
	"io"
)

// Assembly reports bounded progress while preserving every byte in the private
// destination. Offset resumes the same unfinished record; callers own its prefix.
type Assembly struct {
	Offset   int64
	Scanned  int64
	Complete bool
	EOF      bool
}

// Assemble appends up to maxScanBytes from one record to destination. Unlike
// Scan, it never discards a prefix merely because a line or scan is large.
func Assemble(ctx context.Context, source io.ReaderAt, destination io.Writer, offset int64, maxScanBytes int) (Assembly, error) {
	out := Assembly{Offset: offset}
	if offset < 0 || maxScanBytes < 1 {
		return out, errors.New("invalid assembly limits")
	}
	var buffer [4096]byte
	for out.Scanned < int64(maxScanBytes) {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		n, err := source.ReadAt(buffer[:min(len(buffer), maxScanBytes-int(out.Scanned))], out.Offset)
		out.Scanned += int64(n)
		if n == 0 && err == nil {
			return out, io.ErrNoProgress
		}
		part := buffer[:n]
		if newline := bytes.IndexByte(part, '\n'); newline >= 0 {
			part = part[:newline+1]
			out.Complete = true
		}
		written, writeErr := destination.Write(part)
		out.Offset += int64(written)
		if writeErr != nil {
			return out, writeErr
		}
		if written != len(part) {
			return out, io.ErrShortWrite
		}
		if out.Complete {
			return out, nil
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return out, err
			}
			out.EOF = true
			return out, nil
		}
	}
	return out, nil
}
