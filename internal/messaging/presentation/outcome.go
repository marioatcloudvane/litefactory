// Package presentation defines the command-local, redacted data boundary for
// reply narration. It has no command, history, provider or store authority.
package presentation

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// Version identifies the saved outcome format. Unknown formats must fall back
// to a separately saved legacy raw reply, never be interpreted as this format.
const Version = 1

// Bounds apply before persistence or provider invocation. RawText is the
// existing requested result/page, not a reconstructed or shortened report.
const (
	MaxRawBytes      = 64 << 10
	MaxSourceBytes   = 128 << 10
	MaxFacts         = 128
	MaxProtected     = 128
	MaxLanguageBytes = 64
	MaxHandleBytes   = 64
)

// Status is supplied by the outcome producer, never inferred from raw prose.
type Status string

// Command outcome statuses preserve the producer's assessment.
const (
	Success         Status = "success"
	Refusal         Status = "refusal"
	ReportableError Status = "error"
	Uncertain       Status = "uncertain"
)

// Source identifies exact UTF-8 byte spans in the original redacted result.
// Value repeats those bytes so readers can reject stale or altered references.
type Source struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Value string `json:"value"`
}

// Fact is source evidence, not permission to execute a command. Mandatory facts
// include caveats and uncertainty that accepted narration must retain.
type Fact struct {
	Handle    string `json:"handle"`
	Source    Source `json:"source"`
	Mandatory bool   `json:"mandatory"`
}

// ProtectedKind describes exact factory-rendered content. A next step is only
// one already present in the completed command outcome.
type ProtectedKind string

// Protected kinds identify fixed values and parsing-sensitive source blocks.
const (
	ExactValue ProtectedKind = "value"
	Link       ProtectedKind = "link"
	Command    ProtectedKind = "command"
	Revision   ProtectedKind = "revision"
	Citation   ProtectedKind = "citation"
	AskPrefix  ProtectedKind = "ask_prefix"
	PageFooter ProtectedKind = "page_footer"
	NextStep   ProtectedKind = "next_step"
)

// Protected is rendered unchanged. Prefix content such as an ask marker must
// remain before agent-written prose; the model cannot replace it.
type Protected struct {
	Handle string        `json:"handle"`
	Kind   ProtectedKind `json:"kind"`
	Source Source        `json:"source"`
}

// Outcome contains only one completed command's redacted source. Transport
// identities, credentials, repository files and full history are deliberately
// absent. Language is a resolved hint; an empty hint means English.
type Outcome struct {
	Version   int         `json:"version"`
	Status    Status      `json:"status"`
	RawText   string      `json:"raw_text"`
	Facts     []Fact      `json:"facts"`
	Protected []Protected `json:"protected"`
	Language  string      `json:"language"`
}

// Validate rejects unsupported versions, unbounded data and references that
// are not exact source bytes. This does not prove semantic support or redaction:
// producers must redact first and narration still needs an independent check.
func (o Outcome) Validate() error {
	if o.Version != Version {
		return errors.New("presentation: unsupported outcome version")
	}
	switch o.Status {
	case Success, Refusal, ReportableError, Uncertain:
	default:
		return errors.New("presentation: invalid outcome status")
	}
	if strings.TrimSpace(o.RawText) == "" || len(o.RawText) > MaxRawBytes || !utf8.ValidString(o.RawText) {
		return errors.New("presentation: invalid raw result")
	}
	if len(o.Facts) == 0 || len(o.Facts) > MaxFacts || len(o.Protected) > MaxProtected || len(o.Language) > MaxLanguageBytes || !utf8.ValidString(o.Language) || strings.ContainsAny(o.Language, "\r\n\x00") {
		return errors.New("presentation: invalid outcome shape")
	}
	seen := make(map[string]bool, len(o.Facts)+len(o.Protected))
	sourceBytes := len(o.RawText) + len(o.Language)
	check := func(handle string, source Source) error {
		if !validHandle(handle) || seen[handle] {
			return errors.New("presentation: invalid or duplicate source handle")
		}
		seen[handle] = true
		sourceBytes += len(handle) + len(source.Value)
		if sourceBytes > MaxSourceBytes {
			return errors.New("presentation: source evidence exceeds budget")
		}
		if source.Start < 0 || source.End <= source.Start || source.End > len(o.RawText) || source.Value != o.RawText[source.Start:source.End] || !utf8.ValidString(source.Value) {
			return errors.New("presentation: source does not match raw result")
		}
		return nil
	}
	mandatory := false
	for _, f := range o.Facts {
		if err := check(f.Handle, f.Source); err != nil {
			return err
		}
		mandatory = mandatory || f.Mandatory
	}
	if o.Status == Uncertain && !mandatory {
		return errors.New("presentation: uncertain outcome requires a mandatory caveat")
	}
	prefixes := 0
	for _, p := range o.Protected {
		switch p.Kind {
		case ExactValue, Link, Command, Revision, Citation, PageFooter, NextStep:
		case AskPrefix:
			prefixes++
			if p.Source.Start != 0 || prefixes > 1 {
				return errors.New("presentation: invalid ask prefix")
			}
		default:
			return errors.New("presentation: invalid protected kind")
		}
		if err := check(p.Handle, p.Source); err != nil {
			return err
		}
	}
	return nil
}

func validHandle(handle string) bool {
	if len(handle) == 0 || len(handle) > MaxHandleBytes {
		return false
	}
	for _, r := range handle {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

// LanguageHint returns the default without reading or exposing history.
func (o Outcome) LanguageHint() string {
	if strings.TrimSpace(o.Language) == "" {
		return "English"
	}
	return o.Language
}

// Claim is model-written prose with source handles. Protected references point
// to factory-rendered values rather than model-written replacement strings.
type Claim struct {
	Text      string   `json:"text"`
	Facts     []string `json:"facts"`
	Protected []string `json:"protected"`
}

// Candidate is generation output, never a final transport payload.
type Candidate struct {
	Claims []Claim `json:"claims"`
}

// Support is the independent checker's verdict for one claim.
type Support string

// Support verdicts fail closed unless every claim is supported.
const (
	Supported        Support = "supported"
	Unsupported      Support = "unsupported"
	SupportUncertain Support = "uncertain"
)

// Finding identifies one candidate claim by its zero-based index.
type Finding struct {
	Claim   int     `json:"claim"`
	Support Support `json:"support"`
}

// Check must cover every claim exactly once before rendering narration.
type Check struct {
	Findings []Finding `json:"findings"`
}

// State distinguishes a saved command result from a saved final reply. Legacy
// rows have no presentation state and remain deliverable as their saved text.
type State string

// Presentation states distinguish completed narration from raw fallback.
const (
	Pending     State = "pending"
	Narrated    State = "narrated"
	RawFallback State = "raw_fallback"
)

// Attempts is persisted before each provider call. Generation and independent
// checking each have one attempt; delivery retries must reuse the final payload.
type Attempts struct {
	GenerationUsed bool `json:"generation_used"`
	CheckUsed      bool `json:"check_used"`
}

// Attachment is part of the same reply operation, not a separate delivery.
type Attachment struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// Payload is fixed before transport I/O. Raw details must exactly match RawText
// and use the factory-owned filename; unavailable attachment support needs a
// separately persisted raw inline fallback before another delivery attempt.
type Payload struct {
	Content    string      `json:"content"`
	Attachment *Attachment `json:"attachment,omitempty"`
}

// RawFilename is owned by the factory, not generated by a model.
const RawFilename = "raw-details.txt"
