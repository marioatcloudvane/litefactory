package config

// ThinkingLogs is the harness-independent setting for whether agent runs log
// the model's thinking. Providers expose summaries of their thinking, not the
// raw chain of thought; [ThinkingLogsFull] asks each provider for the most
// detailed summary it offers.
type ThinkingLogs string

// The supported thinking log levels.
const (
	ThinkingLogsNone       ThinkingLogs = "none"
	ThinkingLogsSummarized ThinkingLogs = "summarized"
	ThinkingLogsFull       ThinkingLogs = "full"
)

// Valid reports whether t is one of the supported levels.
func (t ThinkingLogs) Valid() bool {
	return t == ThinkingLogsNone || t == ThinkingLogsSummarized || t == ThinkingLogsFull
}

// Enabled reports whether thinking should be requested from the provider.
func (t ThinkingLogs) Enabled() bool {
	return t == ThinkingLogsSummarized || t == ThinkingLogsFull
}
