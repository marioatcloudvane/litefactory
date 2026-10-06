// Package render converts captured built-in roles into native Claude and Codex
// delivery files. Rendering is pure, deterministic and bounded by the invocation
// permission ceiling. Callers own source parsing, hashing and confined writes.
package render
