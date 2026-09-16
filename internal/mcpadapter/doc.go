// Package mcpadapter implements the tools-only MCP 2025-11-25 stdio edge.
// It owns protocol state and bounded connection workers, never credentials,
// workflow acceptance, SQL or host configuration. Run owns interruptible streams.
package mcpadapter
