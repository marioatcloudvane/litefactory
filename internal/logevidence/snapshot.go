package logevidence

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"os"
)

// sourceSnapshot is private to one authorized invocation. Both copy and
// verification are incremental; only an integrity-checked snapshot is readable.
type sourceSnapshot struct {
	run                          int64
	fingerprint, policy, cacheID string
	file                         *os.File
	size, copied, verified       int64
	copyHash, verifyHash         hash.Hash
	digest                       string
	ready                        bool
}

func newSnapshot(size int64) (*sourceSnapshot, error) {
	file, err := os.CreateTemp("", "litefactory-source-private-*")
	if err != nil {
		return nil, err
	}
	return &sourceSnapshot{cacheID: rand.Text(), file: file, size: size, copyHash: sha256.New(), verifyHash: sha256.New()}, nil
}
func (p *sourceSnapshot) close() {
	if p.file != nil {
		name := p.file.Name()
		_ = p.file.Close()
		_ = os.Remove(name)
		p.file = nil
	}
}
func (p *sourceSnapshot) advance(ctx context.Context, source io.ReaderAt, allowance int) (int, error) {
	var buffer [4096]byte
	scanned := 0
	for scanned < allowance && !p.ready {
		if err := ctx.Err(); err != nil {
			return scanned, err
		}
		offset := p.copied
		if p.copied == p.size {
			offset = p.verified
		}
		amount := min(int64(len(buffer)), p.size-offset, int64(allowance-scanned))
		if amount == 0 {
			p.digest = hex.EncodeToString(p.copyHash.Sum(nil))
			if p.digest != hex.EncodeToString(p.verifyHash.Sum(nil)) {
				return scanned, errors.New("source_changed")
			}
			p.ready = true
			break
		}
		n, err := source.ReadAt(buffer[:amount], offset)
		scanned += n
		if n > 0 {
			if p.copied < p.size {
				if _, e := p.file.Write(buffer[:n]); e != nil {
					return scanned, e
				}
				_, _ = p.copyHash.Write(buffer[:n])
				p.copied += int64(n)
			} else {
				_, _ = p.verifyHash.Write(buffer[:n])
				p.verified += int64(n)
			}
		}
		if err != nil && (!errors.Is(err, io.EOF) || offset+int64(n) != p.size) {
			return scanned, err
		}
		if n == 0 {
			return scanned, io.ErrNoProgress
		}
	}
	return scanned, nil
}
