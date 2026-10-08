package mutation

import "time"

// Evidence is the immutable approval context attached to a coordinated
// domain mutation. Policy IDs are the exact rules overridden by the actor.
type Evidence struct {
	ActorID             string
	Reason              string
	Source              string
	CorrelationID       string
	OverriddenPolicyIDs []string
	OccurredAt          time.Time
}
