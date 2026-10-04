package redact

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"sort"
	"strings"

	"github.com/cloudvane/litefactory/internal/secrets"
)

// Options configures [New].
type Options struct {
	// Logger receives the per-class substitution counters and the fail-closed
	// panic line. Nil discards them.
	Logger *slog.Logger

	// Token is the resolved bot credential. Its literal value is stripped from
	// every outbound body. An empty token contributes no literal, which is
	// what the CLI paths that never post want.
	Token secrets.Token

	// Environ is the environment scanned for secret-shaped variables, in
	// os.Environ form ("NAME=value"). Nil means os.Environ(); a non-nil empty
	// slice means "scan nothing", which is what a test that wants only the
	// pattern passes should use.
	Environ []string
}

// Redactor applies the S-7 pattern table to outbound bodies.
//
// It is immutable after construction and safe for concurrent use: the literal
// table and the compiled patterns are read-only, and every pass works on
// per-call state.
type Redactor struct {
	log      *slog.Logger
	literals []literal

	// pass runs the substitution table over one body. It is a field rather
	// than a direct method call for exactly one reason: the fail-closed
	// recover in [Redactor.Apply] is otherwise untestable, because the real
	// pass is pure string work that has no way to panic on demand. An internal
	// test substitutes a panicking pass here. It is unexported, so no caller
	// outside this package can weaken the pass.
	pass func(string) (string, Counts)
}

// literal is an exact string to strip, together with the class it is counted
// under. Exact-string matching is not a heuristic — for the bot token and for
// the daemon's own environment we know the credential, so pattern shape does
// not enter into it.
type literal struct {
	class Class
	value string
}

// New builds a Redactor.
//
// The literal table is snapshotted here. The bot token is resolved once at
// process start and held for the process lifetime, and the daemon's
// environment does not change under it, so there is no reason to re-scan on
// every comment.
//
// This is the one place in this package that calls [secrets.Token.Reveal]. The
// revealed value goes into the literal table and nowhere else — it is never
// logged, never returned, and never reachable through an exported method.
func New(opts Options) *Redactor {
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	environ := opts.Environ
	if environ == nil {
		environ = os.Environ()
	}

	literals := make([]literal, 0, 8)
	if !opts.Token.Empty() {
		literals = append(literals, literal{class: ClassBotToken, value: opts.Token.Reveal()})
	}
	literals = append(literals, secretishEnvValues(environ)...)

	r := &Redactor{log: log, literals: dedupeLiterals(literals)}
	r.pass = r.redact
	return r
}

// secretishEnvValues extracts the values of secret-shaped environment
// variables (S-7).
//
// The name selects, the value is what gets matched — a credential that reached
// run output arrived as a value, not as "NAME=value". The [minEnvValueLen]
// floor is the load-bearing part; see its documentation and AC-4.7.
func secretishEnvValues(environ []string) []literal {
	out := make([]literal, 0, len(environ))
	for _, entry := range environ {
		name, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if !secretishEnvName.MatchString(name) {
			continue
		}
		if len(value) < minEnvValueLen || strings.TrimSpace(value) == "" {
			continue
		}
		out = append(out, literal{class: ClassEnvValue, value: value})
	}
	return out
}

// dedupeLiterals removes duplicate values and orders the table longest first.
//
// Longest first matters: if one credential contains another as a substring —
// which happens the moment the same token is exported under two names, or a URL
// embeds a password — replacing the short one first would leave the long one
// fragmented and unmatchable. The first registration of a value wins its class,
// so a token that is also an environment value is still counted as the bot
// token.
func dedupeLiterals(in []literal) []literal {
	seen := make(map[string]struct{}, len(in))
	out := make([]literal, 0, len(in))
	for _, lit := range in {
		if _, dup := seen[lit.value]; dup {
			continue
		}
		seen[lit.value] = struct{}{}
		out = append(out, lit)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if len(out[i].value) != len(out[j].value) {
			return len(out[i].value) > len(out[j].value)
		}
		return out[i].class < out[j].class
	})
	return out
}

// Apply is the only way to produce a [Body], and it owns the whole
// truncate → redact → assert → hard-truncate sequence.
//
// limit is the forge's comment size limit in bytes. The sequence is:
//
//  1. Truncate to [Budget](limit), leaving [Margin] of headroom, because
//     redaction can grow a body as well as shrink it. The cut lands on a
//     token boundary, not just a rune boundary: cutting first means the pass
//     only ever sees whole tokens, and half a credential matches no pattern
//     and equals no literal. See safeCut.
//  2. Run the literal passes, then the pattern passes.
//  3. Assert: no known literal survived, and the result is within limit.
//  4. If it is still over — a body dense with short matches can grow past the
//     margin — hard-truncate at a rune and line boundary.
//
// The caller passes the limit rather than sequencing this itself, because a
// caller that sequences it itself gets it wrong: redact-then-truncate can cut a
// body that grew, and truncate-then-redact-then-truncate-again is a loop nobody
// bounds correctly the first time.
//
// A returned error means the body must not be posted. The run stays
// un-`reported` and the watermark does not advance (AC-4.9, AC-4.10).
//
// -----------------------------------------------------------------------------
// THE SINGLE recover IN THE TREE LIVES BELOW. DO NOT DELETE IT.
//
// litefactory has exactly one recover, it is this one, and it is documented as
// a global invariant in the implementation plan. A linter or a tidy-up pass
// will eventually flag it as a stray defer/recover around code that "obviously
// cannot panic". Removing it does not produce a crash you would notice: it
// produces a panic that unwinds into whatever the caller's own recovery is, or
// takes the daemon down mid-post, and the failure mode people actually hit is
// an unredacted body going out because the pass stopped halfway. Fail closed
// here or the whole package is decoration.
// -----------------------------------------------------------------------------
func (r *Redactor) Apply(ctx context.Context, text string, limit int) (body Body, err error) {
	// A zero Redactor knows no literals and has no logger. Refuse before the
	// deferred recover is installed, so a nil logger cannot turn the
	// fail-closed path into a second panic.
	if r == nil || r.log == nil || r.pass == nil {
		return Body{}, ErrUninitialized
	}
	if limit <= 0 {
		return Body{}, &LimitError{Limit: limit, Err: ErrInvalidLimit}
	}

	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		// Discard whatever partial body existed. A half-redacted body is
		// indistinguishable from an unredacted one.
		body = Body{}
		// Render the recovered value once, here, and use the rendering for
		// both the error and the log line. fmt.Sprint contains a panic in the
		// value's own String method, so an exotic panic payload cannot turn
		// this recovery into a second, unrecovered panic; and nothing
		// downstream is handed the live value to format again.
		panicErr := &PanicError{
			Value: fmt.Sprint(recovered),
			Type:  fmt.Sprintf("%T", recovered),
			Stack: string(debug.Stack()),
		}
		err = panicErr
		// Loud, at error level, naming the consequence rather than just the
		// fault — this line is what someone reads when a run never reported
		// (AC-4.9). daemon.log is local-only and never redacted (S-7), so the
		// stack is safe to record here.
		r.log.LogAttrs(ctx, slog.LevelError,
			"redaction pass panicked; refusing to post the comment",
			slog.String("panic_type", panicErr.Type),
			slog.String("panic", panicErr.Value),
			slog.Int("input_bytes", len(text)),
			slog.String("stack", panicErr.Stack),
		)
	}()

	// 1. Truncate, with headroom.
	staged, truncated := truncateTo(text, Budget(limit), limit < shortLimit)

	// 2. Redact.
	redacted, counts := r.pass(staged)

	// 3. Assert. A surviving literal means the pass has a bug; refuse rather
	// than post a body we have just proved contains a credential.
	if class, ok := r.residual(redacted); ok {
		r.log.LogAttrs(ctx, slog.LevelError,
			"redaction pass left a known credential in the body; refusing to post the comment",
			slog.String("class", class.String()),
		)
		return Body{}, &ResidualError{Class: class}
	}

	// 4. Hard-truncate if redaction grew the body past the limit after all.
	if len(redacted) > limit {
		var cut bool
		redacted, cut = truncateTo(redacted, limit, limit < shortLimit)
		truncated = truncated || cut
	}
	// The post-condition the forge cares about. Anything left over here is a
	// bug in truncate, not a body worth posting.
	if len(redacted) > limit {
		return Body{}, &LimitError{Limit: limit, Err: fmt.Errorf("%w: body is %d bytes after truncation", ErrRedactionFailed, len(redacted))}
	}

	if counts.Total() > 0 || truncated {
		// The per-class counter S-7 requires. Counts carries integers keyed by
		// Class and nothing else, so this line cannot carry matched text.
		r.log.LogAttrs(ctx, slog.LevelInfo, "redacted outbound comment body",
			slog.Any("redactions", counts),
			slog.Bool("truncated", truncated),
			slog.Int("bytes", len(redacted)),
			slog.Int("limit", limit),
		)
	}

	return Body{text: redacted, counts: counts, truncated: truncated, valid: true}, nil
}

// redact runs the literal passes and then the pattern passes, tallying each.
func (r *Redactor) redact(text string) (string, Counts) {
	var counts Counts

	// Literals first. These are the credentials we actually know about, so
	// attributing them to their own class produces a more useful counter than
	// letting a generic shape claim them.
	for _, lit := range r.literals {
		if n := strings.Count(text, lit.value); n > 0 {
			counts.add(lit.class, n)
			text = strings.ReplaceAll(text, lit.value, Replacement)
		}
	}

	for _, p := range passes {
		matches := p.re.FindAllStringIndex(text, -1)
		if len(matches) == 0 {
			continue
		}
		counts.add(p.class, len(matches))
		text = p.re.ReplaceAllString(text, p.repl)
	}

	return text, counts
}

// residual reports a known literal that survived the pass. It is the
// belt-and-braces half of AC-4.6: pattern coverage is a judgement call, but the
// bot token either is or is not in the outbound body, and that is checkable.
func (r *Redactor) residual(text string) (Class, bool) {
	for _, lit := range r.literals {
		if strings.Contains(text, lit.value) {
			return lit.class, true
		}
	}
	return ClassInvalid, false
}
