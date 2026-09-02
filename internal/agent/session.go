package agent

// SessionRef is one conversation segment's identity as it crosses the
// [Runner] boundary.
//
// It is deliberately two strings rather than one, because the two mean
// different things and become known at different times:
//
//   - Ref is litefactory's own pre-computed identity for the segment —
//     derived by naming.SessionRef from the task ref and the segment
//     ordinal, the same way for every Kind, and known before any process
//     ever runs. It is what a tool whose own conversation handle can be
//     chosen in advance (Claude's --session-id) resumes against directly.
//   - Resume is the tool's own reported conversation handle, present only
//     once a real run has reported one and empty until then. It exists for
//     a tool that only discovers its own handle *after* a run has started —
//     Ref still names the conversation up front, and the handle that run
//     reports is recorded here for every run after it to resume against
//     instead.
//
// A [Runner] implementation whose tool has no such split — Claude's does
// not, since its own --session-id is exactly Ref — simply never populates or
// reads Resume. Neither field names the tool that reads it, which is the
// property this type exists to have.
type SessionRef struct {
	// Ref is litefactory's own derived identity for this segment.
	Ref string

	// Resume is the tool's own reported conversation handle, or "" when
	// none has been reported yet.
	Resume string
}
