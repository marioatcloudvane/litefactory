package daemon

import (
	"regexp"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/report"
)

// agentLabelPrefix is the whole of the agent-selection label vocabulary: one
// label, of the shape lf:agent=<kind>, naming which coding-agent-tool
// implementation this issue's next run should use.
//
// It is built on exactly the same detect-loosely, accept-strictly split
// [toolLabelPrefix] documents for D-6, for the same reason: "lf:agent=codex"
// resolves the codex Kind, and "LF:Agent=codex" is seen (so it counts toward
// ambiguity and is refused rather than silently ignored) but never accepted —
// a maintainer who mistyped the prefix is told so on the thread instead of
// being left to assume the label took effect.
const agentLabelPrefix = "lf:agent="

// agentKindName is the shape of a coding-agent-tool name, and it is
// deliberately the same pattern [toolProfileName] holds a lookup key to: a
// label value is checked for shape before it is ever used as a map key into
// opts.Runners, and long before internal/report is asked to print it back.
var agentKindName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// agentDecision is what the labels on an issue say about which coding-agent
// tool its next run should use.
//
// There are exactly three outcomes and the zero value is the first of them:
// no label, so nothing is resolved, and the run proceeds under the
// configured default agent with argv byte-identical to what it was before
// this feature existed (AC-2.1). Otherwise either Refusal is set and nothing
// runs, or a Kind resolved and Agent carries its name.
//
// This is stage 1 of the four-stage refusal precedence documented in the
// plan brief: it is evaluated before the tools-profile ladder, and when it
// refuses, the tools ladder is never reached in that tick. What a resolved
// decision is used *for* — routing a run to a different [agent.Kind], or
// refusing a mid-conversation switch — belongs to later stories (US-3,
// US-7); this type only carries the ladder's answer.
type agentDecision struct {
	// Refusal is why the run must not happen, or [report.ReplyInvalid] when
	// it may.
	Refusal report.ReplyKind

	// Agent is the resolved coding-agent-tool name, set on exactly two
	// paths: a successful resolution, and the unknown-agent refusal, which is
	// the one refusal allowed to name what it could not find. It has passed
	// [agentKindName] in either case, so it is safe to render, and
	// internal/report checks that for itself before it does (S-8).
	//
	// The malformed and ambiguous refusals leave it empty, and that is the
	// whole mechanism by which they cannot echo label text: there is no field
	// for the text to travel in (S-9).
	Agent string

	// MiscasedPrefix records that this malformed refusal was drawn by a label
	// that was seen case-insensitively but did not carry the prefix byte for
	// byte, as opposed to one whose value was the wrong shape. Mirrors
	// [toolDecision.MiscasedPrefix] exactly, for the same reason.
	MiscasedPrefix bool
}

// refused reports whether this decision forbids the run.
func (d agentDecision) refused() bool { return d.Refusal != report.ReplyInvalid }

// resolveAgent decides which coding-agent tool an issue's labels select for
// its next run.
//
// This reuses [Daemon.resolveTools]'s ladder byte for byte, with one
// substitution: the last rung, which asks whether the well-formed value
// names a configured tool profile, instead asks whether it names an
// [agent.Kind] this daemon has a [agent.Runner] registered for — the
// registry [Daemon.agents] holds, built once at startup (AC-5.6, AC-5.15).
// The rules, in order:
//
//   - No label whose prefix equals lf:agent= ignoring case: nothing is
//     resolved and nothing is set. A label that does not carry the prefix in
//     any casing is not an agent label at all and is passed over in silence,
//     not refused.
//   - More than one such label: ambiguous, and refused. One is never picked
//     and two are never unioned, for exactly the reason two lf:tools= labels
//     never are.
//   - The single label's prefix, byte for byte: it must be the lowercase
//     lf:agent=. Anything else is malformed, and refused.
//   - A value that is not the shape of an agent name, including the empty
//     string: malformed, and refused.
//   - A well-formed value with no [agent.Runner] registered for it: unknown,
//     and refused. An absent or empty registry takes this path too, the
//     agent-selection counterpart of D-5.
//   - A registered Kind: resolved.
//
// The labels are the tick's fresh snapshot and are read again on every tick;
// nothing here is cached, and no API call is made for them.
func (d *Daemon) resolveAgent(labels []string) agentDecision {
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
		if len(label) < len(agentLabelPrefix) {
			continue
		}
		prefix, value := label[:len(agentLabelPrefix)], label[len(agentLabelPrefix):]
		if !strings.EqualFold(prefix, agentLabelPrefix) {
			continue
		}
		found = append(found, seen{exactPrefix: prefix == agentLabelPrefix, value: value})
	}

	// Counted before any of them is judged, for exactly the reason
	// [Daemon.resolveTools] counts before judging: a mis-cased label counts in
	// the total, and choosing which of two competing labels gets to describe
	// the failure would make the refusal depend on the order GitHub happened
	// to return them.
	switch len(found) {
	case 0:
		return agentDecision{}
	case 1:
	default:
		return agentDecision{Refusal: report.ReplyAmbiguousAgentLabels}
	}

	label := found[0]
	if !label.exactPrefix {
		return agentDecision{Refusal: report.ReplyMalformedAgentLabel, MiscasedPrefix: true}
	}
	name := label.value
	if !agentKindName.MatchString(name) {
		return agentDecision{Refusal: report.ReplyMalformedAgentLabel}
	}
	if _, ok := d.agents[agent.Kind(name)]; !ok {
		return agentDecision{Refusal: report.ReplyUnknownAgent, Agent: name}
	}
	return agentDecision{Agent: name}
}
