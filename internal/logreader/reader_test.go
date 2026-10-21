package logreader

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestScanBoundedDiscardAndOffsets(t *testing.T) {
	t.Parallel()
	source := strings.NewReader("123456789012345\n{}\n")
	first, err := Scan(context.Background(), source, Position{}, 2, 8, 10)
	if err != nil {
		t.Fatal(err)
	}
	if first.Scanned != 10 || first.Next.Offset != 10 || !first.Next.Discarding || first.EOF || len(first.Records) != 1 || len(first.Records[0].Data) != 0 {
		t.Fatalf("first: %+v", first)
	}
	second, err := Scan(context.Background(), source, first.Next, 2, 8, 32)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Records) != 2 || second.Records[0].Omission != "oversized" || string(second.Records[1].Data) != "{}\n" || second.Records[1].Start != 16 || second.Records[1].End != 19 {
		t.Fatalf("second: %+v", second)
	}
}

func TestScanWithholdsIncompleteAndHonorsCancellation(t *testing.T) {
	t.Parallel()
	page, err := Scan(context.Background(), strings.NewReader("{}\n{\"secret\":1}"), Position{}, 10, 256, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !page.EOF || len(page.Records) != 2 || page.Records[1].Omission != "incomplete" || page.Records[1].Data != nil {
		t.Fatalf("page: %+v", page)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Scan(ctx, strings.NewReader("{}\n"), Position{}, 10, 256, 1024); !errors.Is(err, context.Canceled) {
		t.Fatalf("error: %v", err)
	}
}

// A one-record page may read ahead into later records. Charge those bytes on
// every call, even though the next cursor stays at the first record boundary.
func TestScanChargesLookaheadAgainstSharedWorkLimit(t *testing.T) {
	t.Parallel()
	source := &countingReaderAt{Reader: strings.NewReader(strings.Repeat("{}\n", 3000))}
	const workLimit = 5000
	first, err := Scan(context.Background(), source, Position{}, 1, 256, workLimit)
	if err != nil {
		t.Fatal(err)
	}
	if first.Scanned != 4096 || first.Next.Offset != 3 || len(first.Records) != 1 {
		t.Fatalf("first scan: %+v", first)
	}
	second, err := Scan(context.Background(), source, first.Next, 1, 256, workLimit-int(first.Scanned))
	if err != nil {
		t.Fatal(err)
	}
	if second.Scanned != 904 || second.Next.Offset != 6 || len(second.Records) != 1 {
		t.Fatalf("second scan: %+v", second)
	}
	if source.readBytes != workLimit || first.Scanned+second.Scanned != int64(source.readBytes) {
		t.Fatalf("physical reads=%d, charged=%d", source.readBytes, first.Scanned+second.Scanned)
	}
}

type countingReaderAt struct {
	*strings.Reader
	readBytes int
}

func (r *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := r.Reader.ReadAt(p, off)
	r.readBytes += n
	return n, err
}
