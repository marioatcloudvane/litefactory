package meter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
)

// CounterSnapshot is supplied by durable storage or a verified resume baseline.
// Identity binds the baseline to its attempt, source, invocation, model, counter
// and epoch. A nil snapshot never means a zero lifetime baseline.
type CounterSnapshot struct {
	Identity string
	Tokens   agent.TokenUsage
}

// Reconciliation describes one atomic ledger transition. Duplicate evidence
// changes nothing. Inclusive evidence is recorded but Additive remains false.
// Caller must atomically persist Identity/Digest, Next and the exclusive delta.
type Reconciliation struct {
	Identity  string
	Digest    string
	Duplicate bool
	Additive  bool
	Delta     agent.TokenUsage
	Next      *CounterSnapshot
}

// ObservationIdentity is a collision-free encoded tuple, not string concatenation
// of provider identifiers. Distinct factory attempts always remain distinct.
func ObservationIdentity(o agent.UsageObservation) string {
	return tuple(o.AttemptID, o.SourceID, o.EventKey)
}

// CounterIdentity prevents baselines leaking across models, children or epochs.
func CounterIdentity(o agent.UsageObservation) string {
	known, model := "unknown", ""
	if o.ActualModel != nil {
		known, model = "known", *o.ActualModel
	}
	return tuple(o.AttemptID, o.SourceID, o.InvocationID, known, model, o.CounterKey, o.CounterEpoch, string(o.Scope))
}

func tuple(values ...string) string {
	// A slice of strings always marshals successfully.
	encoded, _ := json.Marshal(values)
	return string(encoded)
}

// Reconcile processes normalized evidence. acceptedDigest is the previously
// accepted digest looked up by ObservationIdentity, or empty if unseen. The
// consumer must retain all event identities, not merely the latest event, to
// deduplicate arbitrary replay. Loading ledger/checkpoint and committing this
// transition requires one consumer-owned transaction or critical section.
func Reconcile(o agent.UsageObservation, prior *CounterSnapshot, acceptedDigest string) (Reconciliation, error) {
	if o.AttemptID == "" || o.SourceID == "" || o.EventKey == "" {
		return Reconciliation{}, &UsageError{Code: "missing_identity", Dimension: "observation"}
	}
	if o.Mode != agent.CounterIncremental && o.Mode != agent.CounterCumulative {
		return Reconciliation{}, &UsageError{Code: "unsupported_semantics", Dimension: "mode"}
	}
	if o.Scope != agent.UsageExclusive && o.Scope != agent.UsageInclusive {
		return Reconciliation{}, &UsageError{Code: "unsupported_semantics", Dimension: "scope"}
	}
	if o.Scope == agent.UsageExclusive && o.InvocationID == "" {
		return Reconciliation{}, &UsageError{Code: "missing_identity", Dimension: "invocation"}
	}
	if o.ActualModel != nil && (*o.ActualModel == "" || o.ModelProvenance == "") {
		return Reconciliation{}, &UsageError{Code: "missing_model_provenance", Dimension: "model"}
	}
	if err := Validate(o.Tokens); err != nil {
		return Reconciliation{}, err
	}
	encoded, err := json.Marshal(o)
	if err != nil {
		return Reconciliation{}, fmt.Errorf("encode observation: %w", err)
	}
	digest := sha256.Sum256(encoded)
	result := Reconciliation{Identity: ObservationIdentity(o), Digest: hex.EncodeToString(digest[:])}
	if acceptedDigest != "" {
		if acceptedDigest != result.Digest {
			return Reconciliation{}, &UsageError{Code: "conflicting_event", Dimension: "observation"}
		}
		result.Duplicate = true
		return result, nil
	}
	delta := o.Tokens.Clone()
	if o.Mode == agent.CounterCumulative {
		if o.CounterKey == "" || o.CounterEpoch == "" {
			return Reconciliation{}, &UsageError{Code: "missing_identity", Dimension: "counter"}
		}
		identity := CounterIdentity(o)
		if prior == nil {
			return Reconciliation{}, &UsageError{Code: "missing_baseline", Dimension: "counter"}
		}
		if prior.Identity != identity {
			return Reconciliation{}, &UsageError{Code: "baseline_scope_mismatch", Dimension: "counter"}
		}
		delta, err = Delta(o.Tokens, prior.Tokens)
		if err != nil {
			return Reconciliation{}, err
		}
		result.Next = &CounterSnapshot{Identity: identity, Tokens: o.Tokens.Clone()}
	}
	if o.Scope == agent.UsageExclusive {
		result.Additive = true
		result.Delta = delta
	}
	return result, nil
}
