package status

import "github.com/ericlitman/threadbear/internal/state"

type Owner string

const (
	OwnerYou      Owner = "you"
	OwnerAgent    Owner = "agent"
	OwnerExternal Owner = "external"
	OwnerNone     Owner = "none"
)

type Footer struct {
	Status state.TaskStatus
	Owner  Owner
	Action string
}

// FooterRejection labels why ParseFooter declined a footer. Production acts
// on Accepted alone — every rejected footer falls through to stronger
// evidence or classification the same way (R12) — so the reason exists to
// keep the decision tree observable: tests pin each branch and the order
// its gates fire in.
type FooterRejection string

const (
	FooterAccepted                FooterRejection = ""
	FooterAbsent                  FooterRejection = "absent"
	FooterMalformed               FooterRejection = "malformed"
	FooterEmbedded                FooterRejection = "embedded"
	FooterQuoted                  FooterRejection = "quoted"
	FooterStale                   FooterRejection = "stale"
	FooterTurnIncomplete          FooterRejection = "turn_incomplete"
	FooterNewerUserMessage        FooterRejection = "newer_user_message"
	FooterStructuredContradiction FooterRejection = "structured_contradiction"
	FooterInvalidOwnerAction      FooterRejection = "invalid_owner_action"
	FooterWeakAction              FooterRejection = "weak_action"
)

type FooterInput struct {
	Message             string
	LatestTurnCompleted bool
	NewerUserMessage    bool
	Stale               bool
	StructuredStatus    state.TaskStatus
}

type FooterResult struct {
	Footer            Footer
	ClassifierMessage string
	Accepted          bool
	Rejection         FooterRejection
}

type Facts struct {
	WaitingForUser        bool
	RuntimeActive         bool
	StructuredFailure     bool
	HealthyIdleAutomation bool
	Interrupted           bool
	Footer                FooterInput
}

type Resolution struct {
	Status            state.TaskStatus
	Provenance        state.Provenance
	ManagedAction     string
	ClassifierMessage string
	Resolved          bool
}
