package archive

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"math"
	"sort"
	"strconv"
	"unicode/utf8"
)

// Record kinds and format identity.
const (
	KindHeader  = "header"
	KindRow     = "row"
	KindTrailer = "trailer"

	FormatName    = "litefactory-archive"
	FormatVersion = 1
)

// Column value types.
const (
	TypeNull    = "null"
	TypeInteger = "integer"
	TypeReal    = "real"
	TypeText    = "text"
	TypeBlob    = "blob"
)

// Header is the first record of every file. ArchivedAt equals the timestamp in
// the file name.
type Header struct {
	Kind          string  `json:"kind"`
	Format        string  `json:"format"`
	Version       int     `json:"version"`
	ArchivedAt    string  `json:"archived_at"`
	TaskRef       string  `json:"task_ref"`
	Repo          string  `json:"repo"`
	Issue         int     `json:"issue"`
	SessionIDs    []int64 `json:"session_ids"`
	SchemaVersion int     `json:"schema_version"`
}

// Trailer is the last record: row counts and digests per table that has rows,
// and the whole-file digest.
type Trailer struct {
	Kind    string            `json:"kind"`
	Counts  map[string]int64  `json:"counts"`
	Digests map[string]string `json:"digests"`
	Digest  string            `json:"digest"`
}

// Column is one typed cell. Value is nil, int64, float64, string or []byte
// matching Type. Text that is not valid UTF-8 is carried as a blob so it
// survives JSON exactly.
type Column struct {
	Name  string
	Type  string
	Value any
}

// Row is one table row in declared column order.
type Row struct {
	Table   string
	Columns []Column
}

// NewColumn classifies a database driver value.
func NewColumn(name string, v any) (Column, error) {
	switch x := v.(type) {
	case nil:
		return Column{Name: name, Type: TypeNull}, nil
	case int64:
		return Column{Name: name, Type: TypeInteger, Value: x}, nil
	case float64:
		return Column{Name: name, Type: TypeReal, Value: x}, nil
	case string:
		if !utf8.ValidString(x) {
			return Column{Name: name, Type: TypeBlob, Value: []byte(x)}, nil
		}
		return Column{Name: name, Type: TypeText, Value: x}, nil
	case []byte:
		return Column{Name: name, Type: TypeBlob, Value: x}, nil
	default:
		return Column{}, fmt.Errorf("archive: column %q has unsupported value type %T", name, v)
	}
}

type wireColumn struct {
	Name  string          `json:"name"`
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

type wireRow struct {
	Kind    string       `json:"kind"`
	Table   string       `json:"table"`
	Columns []wireColumn `json:"columns"`
}

func encodeValue(c Column) (json.RawMessage, error) {
	switch c.Type {
	case TypeNull:
		return json.RawMessage("null"), nil
	case TypeInteger:
		v, ok := c.Value.(int64)
		if !ok {
			return nil, fmt.Errorf("archive: column %q integer value is %T", c.Name, c.Value)
		}
		return json.RawMessage(strconv.FormatInt(v, 10)), nil
	case TypeReal:
		v, ok := c.Value.(float64)
		if !ok {
			return nil, fmt.Errorf("archive: column %q real value is %T", c.Name, c.Value)
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return json.Marshal(strconv.FormatFloat(v, 'g', -1, 64))
		}
		return json.RawMessage(strconv.FormatFloat(v, 'g', -1, 64)), nil
	case TypeText:
		v, ok := c.Value.(string)
		if !ok {
			return nil, fmt.Errorf("archive: column %q text value is %T", c.Name, c.Value)
		}
		return json.Marshal(v)
	case TypeBlob:
		v, ok := c.Value.([]byte)
		if !ok {
			return nil, fmt.Errorf("archive: column %q blob value is %T", c.Name, c.Value)
		}
		return json.Marshal(base64.StdEncoding.EncodeToString(v))
	default:
		return nil, fmt.Errorf("archive: column %q has unknown type %q", c.Name, c.Type)
	}
}

// EncodeRow returns the canonical single-line encoding of r, without the
// trailing newline.
func EncodeRow(r Row) ([]byte, error) {
	w := wireRow{Kind: KindRow, Table: r.Table, Columns: make([]wireColumn, 0, len(r.Columns))}
	for _, c := range r.Columns {
		v, err := encodeValue(c)
		if err != nil {
			return nil, err
		}
		w.Columns = append(w.Columns, wireColumn{Name: c.Name, Type: c.Type, Value: v})
	}
	return json.Marshal(w)
}

// DecodeRow parses one row record and requires it to re-encode to exactly the
// same bytes, which proves the line is canonical and lossless.
func DecodeRow(line []byte) (Row, error) {
	var w wireRow
	if err := json.Unmarshal(line, &w); err != nil {
		return Row{}, fmt.Errorf("archive: decode row: %w", err)
	}
	if w.Kind != KindRow || w.Table == "" {
		return Row{}, errors.New("archive: record is not a row")
	}
	row := Row{Table: w.Table, Columns: make([]Column, 0, len(w.Columns))}
	for _, wc := range w.Columns {
		c := Column{Name: wc.Name, Type: wc.Type}
		switch wc.Type {
		case TypeNull:
		case TypeInteger:
			v, err := strconv.ParseInt(string(wc.Value), 10, 64)
			if err != nil {
				return Row{}, fmt.Errorf("archive: column %q integer: %w", wc.Name, err)
			}
			c.Value = v
		case TypeReal:
			s := string(wc.Value)
			if len(s) > 0 && s[0] == '"' {
				if err := json.Unmarshal(wc.Value, &s); err != nil {
					return Row{}, fmt.Errorf("archive: column %q real: %w", wc.Name, err)
				}
			}
			v, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return Row{}, fmt.Errorf("archive: column %q real: %w", wc.Name, err)
			}
			c.Value = v
		case TypeText:
			var s string
			if err := json.Unmarshal(wc.Value, &s); err != nil {
				return Row{}, fmt.Errorf("archive: column %q text: %w", wc.Name, err)
			}
			c.Value = s
		case TypeBlob:
			var s string
			if err := json.Unmarshal(wc.Value, &s); err != nil {
				return Row{}, fmt.Errorf("archive: column %q blob: %w", wc.Name, err)
			}
			b, err := base64.StdEncoding.DecodeString(s)
			if err != nil {
				return Row{}, fmt.Errorf("archive: column %q blob: %w", wc.Name, err)
			}
			c.Value = b
		default:
			return Row{}, fmt.Errorf("archive: column %q has unknown type %q", wc.Name, wc.Type)
		}
		row.Columns = append(row.Columns, c)
	}
	again, err := EncodeRow(row)
	if err != nil {
		return Row{}, err
	}
	if string(again) != string(line) {
		return Row{}, errors.New("archive: row is not in canonical form")
	}
	return row, nil
}

// Digester accumulates per-table counts and digests. Rows of one table must be
// added in primary-key order.
type Digester struct {
	tables map[string]*tableDigest
}

type tableDigest struct {
	count int64
	hash  hash.Hash
}

// NewDigester returns an empty accumulator.
func NewDigester() *Digester { return &Digester{tables: make(map[string]*tableDigest)} }

// Add encodes r, folds it into its table's digest and returns the line bytes
// (without newline) that a writer must emit.
func (d *Digester) Add(r Row) ([]byte, error) {
	line, err := EncodeRow(r)
	if err != nil {
		return nil, err
	}
	d.addLine(r.Table, line)
	return line, nil
}

func (d *Digester) addLine(table string, line []byte) {
	t := d.tables[table]
	if t == nil {
		t = &tableDigest{hash: sha256.New()}
		d.tables[table] = t
	}
	_, _ = t.hash.Write(line)
	_, _ = t.hash.Write([]byte{'\n'})
	t.count++
}

// Trailer returns the counts and digests accumulated so far.
func (d *Digester) Trailer() Trailer {
	tr := Trailer{Kind: KindTrailer, Counts: make(map[string]int64, len(d.tables)), Digests: make(map[string]string, len(d.tables))}
	for name, t := range d.tables {
		tr.Counts[name] = t.count
		tr.Digests[name] = hex.EncodeToString(t.hash.Sum(nil))
	}
	tr.Digest = CombineDigests(tr.Digests)
	return tr
}

// CombineDigests is the whole-file digest: SHA-256 over "<table>:<digest>\n"
// for each table sorted by name.
func CombineDigests(digests map[string]string) string {
	names := make([]string, 0, len(digests))
	for n := range digests {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		_, _ = h.Write([]byte(n + ":" + digests[n] + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}
