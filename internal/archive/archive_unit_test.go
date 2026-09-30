package archive_test

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/archive"
)

func TestBuildNameParseName(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 3, 4, 5, 6, 7, 999, time.FixedZone("x", 3600))
	name := archive.BuildName(42, at)
	assert.Equal(t, "42-20260304T040607Z.jsonl.gz", name)

	issue, got, err := archive.ParseName(name)
	require.NoError(t, err)
	assert.Equal(t, 42, issue)
	assert.Equal(t, at.UTC().Truncate(time.Second), got)

	for _, bad := range []string{"", "x-20260304T040607Z.jsonl.gz", "42-20260304T040607Z.jsonl.gz.partial", "42-2026.jsonl.gz", "42-20261304T040607Z.jsonl.gz"} {
		_, _, err := archive.ParseName(bad)
		assert.Error(t, err, bad)
	}
}

func TestTaskDir(t *testing.T) {
	t.Parallel()
	assert.Equal(t, filepath.Join("r", "gh", "o", "p", "7"), archive.TaskDir("r", "gh", "o", "p", 7))
}

func mustCol(t *testing.T, name string, v any) archive.Column {
	t.Helper()
	c, err := archive.NewColumn(name, v)
	require.NoError(t, err)
	return c
}

func TestNewColumn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		v        any
		wantType string
	}{
		{"nil/null", nil, archive.TypeNull},
		{"int64/integer", int64(3), archive.TypeInteger},
		{"float64/real", 3.0, archive.TypeReal},
		{"utf8 string/text", "hi", archive.TypeText},
		{"invalid utf8 string/blob", "\xff\xfe", archive.TypeBlob},
		{"bytes/blob", []byte{1}, archive.TypeBlob},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, err := archive.NewColumn("c", tt.v)
			require.NoError(t, err)
			assert.Equal(t, tt.wantType, c.Type)
		})
	}
	_, err := archive.NewColumn("c", struct{}{})
	assert.Error(t, err)
}

func sampleRows(t *testing.T) []archive.Row {
	t.Helper()
	return []archive.Row{
		{Table: "a", Columns: []archive.Column{
			mustCol(t, "i", int64(1)), mustCol(t, "r", 1.0), mustCol(t, "t", "héllo \"q\"\n"),
			mustCol(t, "b", []byte{0, 1, 2, 255}), mustCol(t, "n", nil), mustCol(t, "bad", "\xff"),
			mustCol(t, "big", int64(math.MaxInt64)), mustCol(t, "nan", math.Inf(1)),
		}},
		{Table: "a", Columns: []archive.Column{mustCol(t, "i", int64(2))}},
		{Table: "b", Columns: []archive.Column{mustCol(t, "r", 0.1)}},
	}
}

func TestEncodeDecodeRow_RoundTripByteIdentical(t *testing.T) {
	t.Parallel()
	for _, r := range sampleRows(t) {
		line, err := archive.EncodeRow(r)
		require.NoError(t, err)
		got, err := archive.DecodeRow(line)
		require.NoError(t, err)
		assert.Equal(t, r, got)
		again, err := archive.EncodeRow(got)
		require.NoError(t, err)
		assert.Equal(t, line, again)
	}
}

func TestEncodeRow_IntegerVersusReal(t *testing.T) {
	t.Parallel()
	i, err := archive.EncodeRow(archive.Row{Table: "t", Columns: []archive.Column{mustCol(t, "x", int64(1))}})
	require.NoError(t, err)
	r, err := archive.EncodeRow(archive.Row{Table: "t", Columns: []archive.Column{mustCol(t, "x", float64(1))}})
	require.NoError(t, err)
	assert.NotEqual(t, i, r)
	gi, err := archive.DecodeRow(i)
	require.NoError(t, err)
	assert.Equal(t, int64(1), gi.Columns[0].Value)
	gr, err := archive.DecodeRow(r)
	require.NoError(t, err)
	assert.Equal(t, float64(1), gr.Columns[0].Value)
}

func TestEncodeRow_RejectsMismatchedValue(t *testing.T) {
	t.Parallel()
	for _, c := range []archive.Column{
		{Name: "x", Type: archive.TypeInteger, Value: "s"},
		{Name: "x", Type: archive.TypeReal, Value: int64(1)},
		{Name: "x", Type: archive.TypeText, Value: 1},
		{Name: "x", Type: archive.TypeBlob, Value: "s"},
		{Name: "x", Type: "weird"},
	} {
		_, err := archive.EncodeRow(archive.Row{Table: "t", Columns: []archive.Column{c}})
		assert.Error(t, err, c.Type)
	}
}

func TestDecodeRow_RejectsBadInput(t *testing.T) {
	t.Parallel()
	for name, line := range map[string]string{
		"not json":      `{`,
		"wrong kind":    `{"kind":"header","table":"t","columns":[]}`,
		"no table":      `{"kind":"row","table":"","columns":[]}`,
		"unknown type":  `{"kind":"row","table":"t","columns":[{"name":"a","type":"zz","value":1}]}`,
		"non canonical": `{"kind":"row","table":"t", "columns":[]}`,
		"bad integer":   `{"kind":"row","table":"t","columns":[{"name":"a","type":"integer","value":1.5}]}`,
		"bad blob":      `{"kind":"row","table":"t","columns":[{"name":"a","type":"blob","value":"!!"}]}`,
	} {
		_, err := archive.DecodeRow([]byte(line))
		assert.Error(t, err, name)
	}
}

func TestDigester(t *testing.T) {
	t.Parallel()
	rows := sampleRows(t)
	d := archive.NewDigester()
	for _, r := range rows {
		_, err := d.Add(r)
		require.NoError(t, err)
	}
	tr := d.Trailer()
	assert.Equal(t, map[string]int64{"a": 2, "b": 1}, tr.Counts)
	assert.Len(t, tr.Digests, 2)
	assert.Equal(t, archive.CombineDigests(tr.Digests), tr.Digest)

	d2 := archive.NewDigester()
	_, _ = d2.Add(rows[1])
	_, _ = d2.Add(rows[0])
	assert.NotEqual(t, tr.Digests["a"], d2.Trailer().Digests["a"], "order matters")
	assert.Equal(t, archive.CombineDigests(map[string]string{"b": "2", "a": "1"}), archive.CombineDigests(map[string]string{"a": "1", "b": "2"}))
}

var stamp = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func header(issue int) archive.Header {
	return archive.Header{ArchivedAt: stamp.Format(time.RFC3339), TaskRef: "gh/o/p#7", Repo: "o/p", Issue: issue, SchemaVersion: 88}
}

func writeArchive(t *testing.T, dir string, rows []archive.Row) archive.Result {
	t.Helper()
	final := filepath.Join(dir, "x", archive.BuildName(7, stamp))
	w, err := archive.Create(final, header(7))
	require.NoError(t, err)
	for _, r := range rows {
		require.NoError(t, w.WriteRow(r))
	}
	res, err := w.Finish()
	require.NoError(t, err)
	return res
}

func TestWriterReader_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rows := sampleRows(t)
	res := writeArchive(t, dir, rows)

	assert.Equal(t, res.FinalPath+".partial", res.TempPath)
	assert.Positive(t, res.Compressed)
	assert.Greater(t, res.Uncompressed, int64(0))

	var got []archive.Row
	c, err := archive.Read(res.TempPath, func(r archive.Row) error { got = append(got, r); return nil })
	require.NoError(t, err)
	assert.Equal(t, rows, got)
	assert.Equal(t, res.Trailer, c.Trailer)
	assert.Equal(t, []int64{}, c.Header.SessionIDs)
	assert.Equal(t, archive.FormatName, c.Header.Format)

}

func TestWriter_Modes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	res := writeArchive(t, dir, sampleRows(t))
	fi, err := os.Stat(res.TempPath)
	require.NoError(t, err)
	assert.Equal(t, archive.FileMode, fi.Mode().Perm())
	di, err := os.Stat(filepath.Dir(res.FinalPath))
	require.NoError(t, err)
	assert.Equal(t, archive.DirMode, di.Mode().Perm())
}

func TestWriter_PublishAndNeverOverwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	res := writeArchive(t, dir, sampleRows(t))
	w2Final := res.FinalPath

	// Publish through a fresh writer path: re-create not possible while partial exists.
	_, err := archive.Create(w2Final, header(7))
	require.Error(t, err, "partial already exists")

	// Publish using a reconstructed flow.
	dir2 := t.TempDir()
	final := filepath.Join(dir2, archive.BuildName(7, stamp))
	w, err := archive.Create(final, header(7))
	require.NoError(t, err)
	_, err = w.Finish()
	require.NoError(t, err)
	require.NoError(t, w.Publish())
	_, err = os.Stat(final + archive.PartialSuffix)
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, err = archive.Verify(final)
	require.NoError(t, err)

	before, err := os.ReadFile(final)
	require.NoError(t, err)
	_, err = archive.Create(final, header(7))
	assert.ErrorIs(t, err, archive.ErrExists)
	after, err := os.ReadFile(final)
	require.NoError(t, err)
	assert.Equal(t, before, after)

	// Publish when final appeared meanwhile.
	final2 := filepath.Join(dir2, archive.BuildName(8, stamp))
	h8 := header(8)
	w3, err := archive.Create(final2, h8)
	require.NoError(t, err)
	_, err = w3.Finish()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(final2, []byte("keep"), 0o600))
	err = w3.Publish()
	assert.ErrorIs(t, err, archive.ErrExists)
	b, _ := os.ReadFile(final2)
	assert.Equal(t, "keep", string(b))
	_, err = os.Stat(final2 + archive.PartialSuffix)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestWriter_PublishBeforeFinish(t *testing.T) {
	t.Parallel()
	final := filepath.Join(t.TempDir(), archive.BuildName(7, stamp))
	w, err := archive.Create(final, header(7))
	require.NoError(t, err)
	assert.Error(t, w.Publish())
	require.NoError(t, w.Abort())
	_, err = os.Stat(final + archive.PartialSuffix)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestWriter_AbortAfterFinishRemovesTemp(t *testing.T) {
	t.Parallel()
	res := writeArchive(t, t.TempDir(), sampleRows(t))
	w, err := archive.Create(filepath.Join(t.TempDir(), archive.BuildName(7, stamp)), header(7))
	require.NoError(t, err)
	require.NoError(t, w.Abort())
	require.Error(t, w.WriteRow(sampleRows(t)[0]))
	_, err = os.Stat(res.TempPath)
	assert.NoError(t, err)
}

func TestCreate_HeaderValidation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tests := []struct {
		name  string
		final string
		h     archive.Header
	}{
		{"bad name", filepath.Join(dir, "nope.jsonl.gz"), header(7)},
		{"issue mismatch", filepath.Join(dir, archive.BuildName(7, stamp)), header(8)},
		{"archived_at mismatch", filepath.Join(dir, archive.BuildName(7, stamp.Add(time.Second))), header(7)},
		{"archived_at not UTC", filepath.Join(dir, archive.BuildName(7, stamp)), func() archive.Header {
			h := header(7)
			h.ArchivedAt = stamp.In(time.FixedZone("x", 3600)).Format(time.RFC3339)
			return h
		}()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := archive.Create(tt.final, tt.h)
			assert.Error(t, err)
		})
	}
}

// rewrite gz content of path with f applied to the decompressed bytes.
func rewrite(t *testing.T, path string, f func([]byte) []byte) {
	t.Helper()
	fh, err := os.Open(path)
	require.NoError(t, err)
	gz, err := gzip.NewReader(fh)
	require.NoError(t, err)
	raw, err := io.ReadAll(gz)
	require.NoError(t, err)
	require.NoError(t, fh.Close())
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err = zw.Write(f(raw))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))
}

func TestVerify_DetectsCorruption(t *testing.T) {
	tests := []struct {
		name string
		mut  func(t *testing.T, path string)
	}{
		{"flipped byte in row", func(t *testing.T, p string) {
			rewrite(t, p, func(b []byte) []byte { return bytes.Replace(b, []byte(`"héllo`), []byte(`"hellO`), 1) })
		}},
		{"dropped row line", func(t *testing.T, p string) {
			rewrite(t, p, func(b []byte) []byte {
				lines := bytes.SplitAfter(b, []byte("\n"))
				return bytes.Join(append(lines[:2:2], lines[3:]...), nil)
			})
		}},
		{"truncated trailer", func(t *testing.T, p string) {
			rewrite(t, p, func(b []byte) []byte {
				lines := bytes.SplitAfter(b, []byte("\n"))
				return bytes.Join(lines[:len(lines)-2], nil)
			})
		}},
		{"data after trailer", func(t *testing.T, p string) {
			rewrite(t, p, func(b []byte) []byte { return append(b, []byte("{}\n")...) })
		}},
		{"not gzip", func(t *testing.T, p string) { require.NoError(t, os.WriteFile(p, []byte("plain"), 0o600)) }},
		{"truncated gzip", func(t *testing.T, p string) {
			b, err := os.ReadFile(p)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(p, b[:len(b)/2], 0o600))
		}},
		{"header issue mismatch", func(t *testing.T, p string) {
			rewrite(t, p, func(b []byte) []byte { return bytes.Replace(b, []byte(`"issue":7`), []byte(`"issue":9`), 1) })
		}},
		{"unsupported version", func(t *testing.T, p string) {
			rewrite(t, p, func(b []byte) []byte { return bytes.Replace(b, []byte(`"version":1`), []byte(`"version":2`), 1) })
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := writeArchive(t, t.TempDir(), sampleRows(t))
			_, err := archive.Verify(res.TempPath)
			require.NoError(t, err)
			tt.mut(t, res.TempPath)
			_, err = archive.Verify(res.TempPath)
			assert.Error(t, err)
		})
	}
}

func TestRead_CallbackErrorPropagates(t *testing.T) {
	t.Parallel()
	res := writeArchive(t, t.TempDir(), sampleRows(t))
	boom := errors.New("boom")
	_, err := archive.Read(res.TempPath, func(archive.Row) error { return boom })
	assert.ErrorIs(t, err, boom)
	_, err = archive.Read(filepath.Join(t.TempDir(), "missing"), nil)
	assert.Error(t, err)
}

func TestCleanPartials(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	n, err := archive.CleanPartials(filepath.Join(root, "missing"))
	require.NoError(t, err)
	assert.Zero(t, n)

	a := filepath.Join(root, "g", "o", "r", "1")
	require.NoError(t, os.MkdirAll(a, 0o700))
	keep := filepath.Join(a, archive.BuildName(1, stamp))
	require.NoError(t, os.WriteFile(keep, []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(keep+".partial", []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "g", "z.partial"), []byte("x"), 0o600))

	n, err = archive.CleanPartials(root)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	_, err = os.Stat(keep)
	assert.NoError(t, err)
}

func TestTaskFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	got, err := archive.TaskFiles(filepath.Join(dir, "missing"))
	require.NoError(t, err)
	assert.Empty(t, got)

	n1, n2 := archive.BuildName(3, stamp), archive.BuildName(3, stamp.Add(time.Hour))
	for _, n := range []string{n2, n1, n1 + ".partial", "junk.txt"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, n), nil, 0o600))
	}
	require.NoError(t, os.Mkdir(filepath.Join(dir, archive.BuildName(3, stamp.Add(2*time.Hour))), 0o700))
	got, err = archive.TaskFiles(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(dir, n1), filepath.Join(dir, n2)}, got)
}
