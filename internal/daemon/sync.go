package daemon

import "strings"

// syncLabelPrefix is the per-issue override for whether this turn's
// automatic base-branch sync runs before it starts (US-7). It is built on
// the same detect-loosely, accept-strictly ladder [agentLabelPrefix] and
// [modeLabelPrefix] use, and deliberately not on [toolLabelPrefix]'s: unlike
// that dead label, lf:sync='s resolved value is read and threaded onward,
// never merely detected so it can be refused.
const syncLabelPrefix = "lf:sync="

// syncRefusal is why a lf:sync= label failed to resolve. It carries no label
// text of its own (SEC-5/S-9) — it only names which rung of the ladder
// rejected the label, so a later disclosure story (US-3) can render a reason
// without this package growing an opinion on how that reason is worded.
type syncRefusal int

const (
	// syncRefusalNone is the zero value: either no lf:sync= label was
	// present, or the single label present resolved cleanly.
	// [syncDecision.Resolved] is what distinguishes those two.
	syncRefusalNone syncRefusal = iota
	// syncRefusalAmbiguous means more than one lf:sync= label matched the
	// prefix case-insensitively. Never unioned, never first-wins.
	syncRefusalAmbiguous
	// syncRefusalMalformed means the single matching label's prefix was not
	// byte-exact, or its value was not exactly "on" or "off".
	// [syncDecision.MiscasedPrefix] distinguishes those two causes.
	syncRefusalMalformed
)

// syncDecision is what an issue's labels say about whether this turn's
// automatic base-branch sync should run, overriding the configured default.
//
// The zero value means nothing resolved — no lf:sync= label was present, or
// the one that was present could not be resolved — and the config default
// applies unchanged (AC-7.1). Resolved is set only on a clean resolution,
// and Value then carries the label's outcome: true for "on", false for
// "off". lf:sync=off's outcome is a skip, not merely "do not merge" — see
// US-7's architecture notes; this type only carries the resolved answer,
// not what a caller does with it.
type syncDecision struct {
	// Resolved is true only when a single, byte-exact-prefixed lf:sync=
	// label carried exactly "on" or "off". False covers both silence (no
	// matching label) and every refusal below — the config default applies
	// on this path either way, never a guessed value.
	Resolved bool
	// Value is the label's outcome when Resolved is true: true for "on",
	// false for "off". Meaningless when Resolved is false.
	Value bool
	// Refusal is why a label failed to resolve, or [syncRefusalNone] when
	// nothing was refused.
	Refusal syncRefusal
	// MiscasedPrefix marks the malformed refusal drawn by a label seen
	// case-insensitively but not byte-exact, as opposed to one whose value
	// was the wrong shape. Mirrors [agentDecision.MiscasedPrefix] exactly,
	// for the same reason: no field on this type ever carries the raw label
	// text onward.
	MiscasedPrefix bool
}

// refused reports whether a lf:sync= label was present but could not be
// resolved — distinct from silence (no label at all) and from a clean
// resolution.
func (d syncDecision) refused() bool { return d.Refusal != syncRefusalNone }

// resolveSync decides whether an issue's labels override the configured
// automatic-sync default for this turn.
//
// This copies [Daemon.resolveAgent]'s ladder rung for rung, not
// [legacyToolsLabel]'s: unlike that dead label, lf:sync='s value is read and
// used, never merely detected in order to be refused. The rules, in order:
//
//   - No label whose prefix equals lf:sync=, ignoring case: nothing is
//     resolved. This is silence, not a refusal — the config default
//     applies.
//   - More than one such label: ambiguous, and refused.
//   - The single label's prefix, byte for byte: it must be the lowercase
//     lf:sync=. Anything else is malformed, refused, and flagged
//     MiscasedPrefix.
//   - A value other than exactly "on" or "off", including the empty
//     string: malformed, and refused.
//   - Otherwise: resolved, and Value carries the label's outcome.
//
// The labels are the tick's fresh snapshot and are read again on every
// tick; nothing here is cached, and no API call is made for them.
func resolveSync(labels []string) syncDecision {
	// A label that was seen, with the two facts the rules below need about
	// it: whether the prefix survived a byte comparison as well as a folded
	// one, and what followed it. Neither the label nor this struct leaves
	// this function.
	type seen struct {
		exactPrefix bool
		value       string
	}

	var found []seen
	for _, label := range labels {
		if len(label) < len(syncLabelPrefix) {
			continue
		}
		prefix, value := label[:len(syncLabelPrefix)], label[len(syncLabelPrefix):]
		if !strings.EqualFold(prefix, syncLabelPrefix) {
			continue
		}
		found = append(found, seen{exactPrefix: prefix == syncLabelPrefix, value: value})
	}

	// Counted before any of them is judged, for exactly the reason
	// [Daemon.resolveAgent] counts before judging: a mis-cased label counts
	// toward the total, and letting one of two competing labels describe the
	// failure would make the refusal depend on the order the forge happened
	// to return them in.
	switch len(found) {
	case 0:
		return syncDecision{}
	case 1:
	default:
		return syncDecision{Refusal: syncRefusalAmbiguous}
	}

	label := found[0]
	if !label.exactPrefix {
		return syncDecision{Refusal: syncRefusalMalformed, MiscasedPrefix: true}
	}
	switch label.value {
	case "on":
		return syncDecision{Resolved: true, Value: true}
	case "off":
		return syncDecision{Resolved: true, Value: false}
	default:
		return syncDecision{Refusal: syncRefusalMalformed}
	}
}
