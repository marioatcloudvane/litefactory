// Package archive is the pure file format of the per-task archive: a gzip
// compressed JSON Lines file holding one header record, typed table rows in
// declared column order, and one trailer record with per-table counts and
// SHA-256 digests.
//
// The package is a leaf. It performs no database access and knows no table
// names: callers stream rows in, read rows back, and compare digests. A row's
// digest contribution is the SHA-256 of its exact encoded line (including the
// newline), so the writer, the reader and the store's live-row recomputation
// all use [EncodeRow] and cannot drift apart. Rows of one table must be added
// in primary-key order; the per-table digest is the digest of that sequence.
// The whole-file digest is SHA-256 over "<table>:<table digest>\n" for the
// tables that have rows, sorted by table name.
//
// File layout on disk is <root>/<forge>/<owner>/<repo>/<issue>/ with files named
// <issue>-<YYYYMMDDTHHMMSSZ>.jsonl.gz, mode 0600 inside directories 0700. Files
// are written to "<name>.partial", verified by the caller, and published by an
// exclusive link so an existing archive is never overwritten.
package archive
