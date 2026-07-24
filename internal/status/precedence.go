package status

import "github.com/ericlitman/threadbear/internal/state"

type precedenceRule struct {
	matches func(Facts) bool
	resolve func(FooterResult) Resolution
}

var precedence = []precedenceRule{
	{
		matches: func(f Facts) bool { return f.WaitingForUser },
		resolve: func(footer FooterResult) Resolution {
			return resolved(state.StatusNeedsInput, state.ProvenanceRuntime, "", footer)
		},
	},
	{
		matches: func(f Facts) bool { return f.RuntimeActive },
		resolve: func(footer FooterResult) Resolution {
			return resolved(state.StatusRunning, state.ProvenanceRuntime, "", footer)
		},
	},
	{
		matches: func(f Facts) bool { return f.StructuredFailure },
		resolve: func(footer FooterResult) Resolution {
			return resolved(state.StatusBlocked, state.ProvenanceStructuredError, "", footer)
		},
	},
	{
		matches: func(f Facts) bool { return f.HealthyIdleAutomation },
		resolve: func(footer FooterResult) Resolution {
			return resolved(state.StatusAutomation, state.ProvenanceAutomation, "", footer)
		},
	},
	{
		matches: func(f Facts) bool { return f.Interrupted },
		resolve: func(footer FooterResult) Resolution {
			return resolved(state.StatusUnknown, state.ProvenanceInterruption, "", footer)
		},
	},
}

func Resolve(facts Facts) Resolution {
	footer := ParseFooter(facts.Footer)
	for _, rule := range precedence {
		if rule.matches(facts) {
			return rule.resolve(footer)
		}
	}
	if footer.Accepted {
		action := footer.Footer.Action
		if footer.Footer.Owner == OwnerNone {
			action = ""
		}
		return resolved(footer.Footer.Status, state.ProvenanceFooter, action, footer)
	}
	return Resolution{ClassifierMessage: footer.ClassifierMessage, TitleMessage: footer.TitleMessage}
}

func resolved(taskStatus state.TaskStatus, provenance state.Provenance, action string, footer FooterResult) Resolution {
	return Resolution{
		Status:            taskStatus,
		Provenance:        provenance,
		ManagedAction:     action,
		ClassifierMessage: footer.ClassifierMessage,
		TitleMessage:      footer.TitleMessage,
		Resolved:          true,
	}
}
