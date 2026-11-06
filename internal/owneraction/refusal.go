package owneraction

// Refusal is a domain refusal whose Reason is a fixed factory sentence, never
// client input, that a transport may show the owner as it is.
type Refusal struct{ Reason string }

func (r Refusal) Error() string { return r.Reason }
