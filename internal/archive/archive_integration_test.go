package archive_test

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/archive"
)

var archiveITAt = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

func archiveITRows() []archive.Row {
	col := func(n, ty string, v any) archive.Column { return archive.Column{Name: n, Type: ty, Value: v} }
	return []archive.Row{
		{Table: "a", Columns: []archive.Column{col("id", archive.TypeInteger, int64(math.MaxInt64)), col("v", archive.TypeReal, 1.0), col("n", archive.TypeNull, nil)}},
		{Table: "a", Columns: []archive.Column{col("id", archive.TypeInteger, int64(math.MinInt64)), col("v", archive.TypeReal, math.Copysign(0, -1)), col("n", archive.TypeText, "line\nbreak \"q\" \u2028 \u00e9")}},
		{Table: "a", Columns: []archive.Column{col("id", archive.TypeInteger, int64(9007199254740993)), col("v", archive.TypeReal, math.SmallestNonzeroFloat64), col("n", archive.TypeBlob, []byte{0, 1, 0xff, 0xfe})}},
		{Table: "b", Columns: []archive.Column{col("v", archive.TypeReal, math.Inf(1)), col("w", archive.TypeReal, math.Inf(-1)), col("b", archive.TypeBlob, []byte{})}},
		{Table: "b", Columns: []archive.Column{col("v", archive.TypeReal, 0.1), col("w", archive.TypeInteger, int64(1)), col("b", archive.TypeBlob, bytes.Repeat([]byte{0xab}, 100000))}},
	}
}

func archiveITWrite(t *testing.T, dir string, rows []archive.Row) (string, archive.Result) {
	t.Helper()
	final := filepath.Join(dir, archive.BuildName(42, archiveITAt))
	w, err := archive.Create(final, archive.Header{ArchivedAt: archiveITAt.Format(time.RFC3339), TaskRef: "github:acme/api#42", Repo: "acme/api", Issue: 42, SessionIDs: []int64{1}, SchemaVersion: 7})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if err := w.WriteRow(r); err != nil {
			t.Fatal(err)
		}
	}
	res, err := w.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(res.TempPath); err != nil {
		t.Fatalf("temp missing after finish: %v", err)
	}
	if _, err := os.Stat(final); err == nil {
		t.Fatal("final exists before publish")
	}
	if err := w.Publish(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(res.TempPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("temp survived publish")
	}
	return final, res
}

func TestArchiveIntegrationWriterReaderRoundTrip(t *testing.T) {
	dir := t.TempDir()
	rows := archiveITRows()
	final, res := archiveITWrite(t, dir, rows)
	var got []archive.Row
	c, err := archive.Read(final, func(r archive.Row) error { got = append(got, r); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(rows) {
		t.Fatalf("rows %d", len(got))
	}
	for i := range rows {
		if !reflect.DeepEqual(got[i], rows[i]) {
			t.Fatalf("row %d:\n got %#v\nwant %#v", i, got[i], rows[i])
		}
		if archiveITIsNegZero(rows[i]) && !archiveITIsNegZero(got[i]) {
			t.Fatal("negative zero lost")
		}
	}
	if !reflect.DeepEqual(c.Trailer, res.Trailer) || c.Trailer.Counts["a"] != 3 || c.Trailer.Counts["b"] != 2 {
		t.Fatalf("trailer %+v vs %+v", c.Trailer, res.Trailer)
	}
	if c.Header.SchemaVersion != 7 || c.Header.Issue != 42 {
		t.Fatalf("header %+v", c.Header)
	}
	if res.Uncompressed <= res.Compressed || res.Compressed <= 0 {
		t.Fatalf("sizes %d/%d", res.Uncompressed, res.Compressed)
	}
	if info, _ := os.Stat(final); info.Size() != res.Compressed {
		t.Fatalf("compressed size %d != file %d", res.Compressed, info.Size())
	}
	// Re-encoding what was read reproduces the stored digests exactly.
	d := archive.NewDigester()
	for _, r := range got {
		if _, err := d.Add(r); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(d.Trailer(), res.Trailer) {
		t.Fatal("re-digest differs")
	}
}

func archiveITIsNegZero(r archive.Row) bool {
	for _, c := range r.Columns {
		if f, ok := c.Value.(float64); ok && f == 0 && math.Signbit(f) {
			return true
		}
	}
	return false
}

func TestArchiveIntegrationIntegerAndRealStayDistinct(t *testing.T) {
	dir := t.TempDir()
	rows := []archive.Row{{Table: "t", Columns: []archive.Column{
		{Name: "i", Type: archive.TypeInteger, Value: int64(1)},
		{Name: "r", Type: archive.TypeReal, Value: float64(1)},
	}}}
	final, _ := archiveITWrite(t, dir, rows)
	var got []archive.Row
	if _, err := archive.Read(final, func(r archive.Row) error { got = append(got, r); return nil }); err != nil {
		t.Fatal(err)
	}
	if got[0].Columns[0].Type != archive.TypeInteger || got[0].Columns[1].Type != archive.TypeReal {
		t.Fatalf("types collapsed: %+v", got[0])
	}
	if _, ok := got[0].Columns[0].Value.(int64); !ok {
		t.Fatal("integer not int64")
	}
	if _, ok := got[0].Columns[1].Value.(float64); !ok {
		t.Fatal("real not float64")
	}
}

func archiveITGunzip(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(gz)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func archiveITGzipTo(t *testing.T, path string, b []byte) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, _ = gz.Write(b)
	_ = gz.Close()
	if err := os.WriteFile(path, buf.Bytes(), archive.FileMode); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveIntegrationCorruptionIsDetected(t *testing.T) {
	good, _ := archiveITWrite(t, t.TempDir(), archiveITRows())
	raw, err := os.ReadFile(good)
	if err != nil {
		t.Fatal(err)
	}
	plain := archiveITGunzip(t, good)
	lines := strings.Split(strings.TrimSuffix(string(plain), "\n"), "\n")

	cases := map[string]func(dir string) string{
		"empty file":         func(d string) string { return archiveITPut(t, d, nil) },
		"truncated at half":  func(d string) string { return archiveITPut(t, d, raw[:len(raw)/2]) },
		"truncated one byte": func(d string) string { return archiveITPut(t, d, raw[:len(raw)-1]) },
		"byte flipped middle": func(d string) string {
			b := append([]byte(nil), raw...)
			b[len(b)/2] ^= 0x01
			return archiveITPut(t, d, b)
		},
		"byte flipped header": func(d string) string { b := append([]byte(nil), raw...); b[12] ^= 0x40; return archiveITPut(t, d, b) },
		"garbage appended": func(d string) string {
			return archiveITPut(t, d, append(append([]byte(nil), raw...), []byte("junk")...))
		},
		"not gzip": func(d string) string { return archiveITPut(t, d, []byte("hello")) },
		"valid gzip, trailer removed": func(d string) string {
			p := archiveITPut(t, d, nil)
			archiveITGzipTo(t, p, []byte(strings.Join(lines[:len(lines)-1], "\n")+"\n"))
			return p
		},
		"valid gzip, row dropped": func(d string) string {
			p := archiveITPut(t, d, nil)
			archiveITGzipTo(t, p, []byte(strings.Join(append(append([]string{}, lines[:2]...), lines[3:]...), "\n")+"\n"))
			return p
		},
		"valid gzip, row edited": func(d string) string {
			p := archiveITPut(t, d, nil)
			edited := append([]string(nil), lines...)
			edited[1] = strings.Replace(edited[1], "9223372036854775807", "9223372036854775806", 1)
			if edited[1] == lines[1] {
				t.Fatal("edit did not apply")
			}
			archiveITGzipTo(t, p, []byte(strings.Join(edited, "\n")+"\n"))
			return p
		},
		"valid gzip, rows swapped": func(d string) string {
			p := archiveITPut(t, d, nil)
			swapped := append([]string(nil), lines...)
			swapped[1], swapped[2] = swapped[2], swapped[1]
			archiveITGzipTo(t, p, []byte(strings.Join(swapped, "\n")+"\n"))
			return p
		},
		"valid gzip, data after trailer": func(d string) string {
			p := archiveITPut(t, d, nil)
			archiveITGzipTo(t, p, append(append([]byte(nil), plain...), []byte(lines[1]+"\n")...))
			return p
		},
		"valid gzip, non-canonical row": func(d string) string {
			p := archiveITPut(t, d, nil)
			edited := append([]string(nil), lines...)
			edited[1] = strings.Replace(edited[1], `"kind":"row"`, `"kind": "row"`, 1)
			archiveITGzipTo(t, p, []byte(strings.Join(edited, "\n")+"\n"))
			return p
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			p := mk(t.TempDir())
			if _, err := archive.Verify(p); err == nil {
				t.Fatal("corruption not detected")
			}
		})
	}
	// Control: the unmodified file verifies, and so does a recompressed copy.
	if _, err := archive.Verify(good); err != nil {
		t.Fatal(err)
	}
	p := archiveITPut(t, t.TempDir(), nil)
	archiveITGzipTo(t, p, plain)
	if _, err := archive.Verify(p); err != nil {
		t.Fatalf("control failed: %v", err)
	}
}

func archiveITPut(t *testing.T, dir string, b []byte) string {
	t.Helper()
	p := filepath.Join(dir, archive.BuildName(42, archiveITAt))
	if err := os.WriteFile(p, b, archive.FileMode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestArchiveIntegrationRenamedFileFailsHeaderBinding(t *testing.T) {
	dir := t.TempDir()
	good, _ := archiveITWrite(t, dir, archiveITRows())
	other := filepath.Join(dir, archive.BuildName(43, archiveITAt))
	if err := os.Rename(good, other); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Verify(other); err == nil {
		t.Fatal("file bound to another issue verified")
	}
	later := filepath.Join(dir, archive.BuildName(42, archiveITAt.Add(time.Second)))
	if err := os.Rename(other, later); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Verify(later); err == nil {
		t.Fatal("file with another timestamp verified")
	}
}

// Publish never replaces an existing final file, and the temporary file goes.
func TestArchiveIntegrationPublishRefusesExistingTargetAndRemovesTemp(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, archive.BuildName(42, archiveITAt))
	h := archive.Header{ArchivedAt: archiveITAt.Format(time.RFC3339), TaskRef: "x", Repo: "acme/api", Issue: 42}
	w, err := archive.Create(final, h)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteRow(archiveITRows()[0]); err != nil {
		t.Fatal(err)
	}
	res, err := w.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(final, []byte("precious"), archive.FileMode); err != nil {
		t.Fatal(err)
	}
	if err := w.Publish(); !errors.Is(err, archive.ErrExists) {
		t.Fatalf("publish err %v", err)
	}
	if b, _ := os.ReadFile(final); string(b) != "precious" {
		t.Fatal("existing target replaced")
	}
	if _, err := os.Stat(res.TempPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("temp not removed after failed publish")
	}
	// Create also refuses when the target exists, without leaving a temp.
	if _, err := archive.Create(final, h); !errors.Is(err, archive.ErrExists) {
		t.Fatalf("create err %v", err)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Fatalf("directory has %d entries", len(ents))
	}
}

func TestArchiveIntegrationAbortAndCleanPartials(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "github", "acme", "api", "42")
	final := filepath.Join(dir, archive.BuildName(42, archiveITAt))
	h := archive.Header{ArchivedAt: archiveITAt.Format(time.RFC3339), TaskRef: "x", Repo: "acme/api", Issue: 42}

	w, err := archive.Create(final, h)
	if err != nil {
		t.Fatal(err)
	}
	_ = w.WriteRow(archiveITRows()[0])
	if err := w.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(final + archive.PartialSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("abort left temp")
	}
	if _, err := os.Stat(final); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("abort published")
	}

	// Abort after Finish (a failed verification) also removes the temp.
	w, err = archive.Create(final, h)
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(res.TempPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("abort after finish left temp")
	}

	// Two interrupted runs leave partials; a published file is untouched.
	good, _ := archiveITWrite(t, dir, archiveITRows())
	for _, p := range []string{filepath.Join(dir, "1-20260101T000000Z.jsonl.gz.partial"), filepath.Join(root, "github", "zzz.partial")} {
		if err := os.MkdirAll(filepath.Dir(p), archive.DirMode); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), archive.FileMode); err != nil {
			t.Fatal(err)
		}
	}
	n, err := archive.CleanPartials(root)
	if err != nil || n != 2 {
		t.Fatalf("removed %d, %v", n, err)
	}
	if _, err := archive.Verify(good); err != nil {
		t.Fatal(err)
	}
	if n, err := archive.CleanPartials(filepath.Join(root, "missing")); err != nil || n != 0 {
		t.Fatalf("missing root: %d %v", n, err)
	}
}

func TestArchiveIntegrationTaskFilesIgnoresPartialsAndForeignNames(t *testing.T) {
	dir := t.TempDir()
	good, _ := archiveITWrite(t, dir, archiveITRows())
	for _, n := range []string{"notes.txt", "42-20260601T120000Z.jsonl.gz.partial", "42-bad.jsonl.gz"} {
		_ = os.WriteFile(filepath.Join(dir, n), []byte("x"), archive.FileMode)
	}
	files, err := archive.TaskFiles(dir)
	if err != nil || len(files) != 1 || files[0] != good {
		t.Fatalf("files %v %v", files, err)
	}
	if files, err := archive.TaskFiles(filepath.Join(dir, "absent")); err != nil || files != nil {
		t.Fatalf("absent dir: %v %v", files, err)
	}
}
