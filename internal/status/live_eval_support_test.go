package status

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ericlitman/threadbear/internal/state"
)

const (
	liveEvalSchemaRevision = "threadbear.live-eval.v1"
	liveEvalRevision       = "live-eval"
)

type liveEvalCorpus struct {
	SchemaRevision string         `json:"schema_revision"`
	Cases          []liveEvalCase `json:"cases"`
}

type liveEvalCase struct {
	ID         string             `json:"id"`
	Expected   state.TaskStatus   `json:"expected"`
	Provenance liveEvalProvenance `json:"provenance"`
	Facts      *liveEvalFacts     `json:"facts"`
	Latest     *TurnEvidence      `json:"latest"`
	Previous   *TurnEvidence      `json:"previous,omitempty"`
}

type liveEvalProvenance struct {
	Model              string `json:"model"`
	Effort             string `json:"effort"`
	Source             string `json:"source"`
	AgentsBlockVersion string `json:"agents_block_version"`
}

type liveEvalFacts struct {
	WaitingForUser        bool                 `json:"waiting_for_user,omitempty"`
	RuntimeActive         bool                 `json:"runtime_active,omitempty"`
	StructuredFailure     bool                 `json:"structured_failure,omitempty"`
	HealthyIdleAutomation bool                 `json:"healthy_idle_automation,omitempty"`
	Interrupted           bool                 `json:"interrupted,omitempty"`
	Footer                *liveEvalFooterInput `json:"footer"`
}

type liveEvalFooterInput struct {
	Message             string           `json:"message"`
	LatestTurnCompleted bool             `json:"latest_turn_completed"`
	NewerUserMessage    bool             `json:"newer_user_message,omitempty"`
	Stale               bool             `json:"stale,omitempty"`
	StructuredStatus    state.TaskStatus `json:"structured_status,omitempty"`
}

func (f liveEvalFacts) productionFacts() Facts {
	return Facts{
		WaitingForUser:        f.WaitingForUser,
		RuntimeActive:         f.RuntimeActive,
		StructuredFailure:     f.StructuredFailure,
		HealthyIdleAutomation: f.HealthyIdleAutomation,
		Interrupted:           f.Interrupted,
		Footer: FooterInput{
			Message:             f.Footer.Message,
			LatestTurnCompleted: f.Footer.LatestTurnCompleted,
			NewerUserMessage:    f.Footer.NewerUserMessage,
			Stale:               f.Footer.Stale,
			StructuredStatus:    f.Footer.StructuredStatus,
		},
	}
}

type liveEvalStateScore struct {
	Total   int `json:"total"`
	Correct int `json:"correct"`
}

type liveEvalError struct {
	ID       string           `json:"id"`
	Expected state.TaskStatus `json:"expected"`
	Actual   state.TaskStatus `json:"actual"`
}

type liveEvalProvenanceScore struct {
	Total           int `json:"total"`
	FooterAccepted  int `json:"footer_accepted"`
	FooterRejected  int `json:"footer_rejected"`
	Deterministic   int `json:"deterministic"`
	ClassifierOwned int `json:"classifier_owned"`
}

type liveEvalReport struct {
	SchemaRevision string                                  `json:"schema_revision"`
	Total          int                                     `json:"total"`
	Correct        int                                     `json:"correct"`
	Deterministic  int                                     `json:"deterministic"`
	Classified     int                                     `json:"classified"`
	FalseComplete  int                                     `json:"false_complete"`
	FalseNextSteps int                                     `json:"false_next_steps"`
	ByState        map[state.TaskStatus]liveEvalStateScore `json:"by_state"`
	ByProvenance   map[string]liveEvalProvenanceScore      `json:"by_provenance"`
	Errors         []liveEvalError                         `json:"errors"`
}

func decodeLiveEvalCorpus(data []byte) (liveEvalCorpus, error) {
	var corpus liveEvalCorpus
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&corpus); err != nil {
		return liveEvalCorpus{}, fmt.Errorf("decode corpus: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return liveEvalCorpus{}, err
	}
	if corpus.SchemaRevision != liveEvalSchemaRevision {
		return liveEvalCorpus{}, fmt.Errorf("schema_revision=%q want %q", corpus.SchemaRevision, liveEvalSchemaRevision)
	}
	if len(corpus.Cases) == 0 {
		return liveEvalCorpus{}, errors.New("corpus contains no cases")
	}
	seen := make(map[string]bool, len(corpus.Cases))
	for index, item := range corpus.Cases {
		if strings.TrimSpace(item.ID) != item.ID || item.ID == "" {
			return liveEvalCorpus{}, fmt.Errorf("case %d has invalid id", index)
		}
		if seen[item.ID] {
			return liveEvalCorpus{}, fmt.Errorf("duplicate case id %q", item.ID)
		}
		seen[item.ID] = true
		if !item.Expected.Valid() {
			return liveEvalCorpus{}, fmt.Errorf("case %q has invalid expected state %q", item.ID, item.Expected)
		}
		if item.Facts == nil || item.Facts.Footer == nil || item.Latest == nil {
			return liveEvalCorpus{}, fmt.Errorf("case %q must include facts, facts.footer, and latest objects", item.ID)
		}
		provenance := item.Provenance
		if strings.TrimSpace(provenance.Model) == "" ||
			strings.TrimSpace(provenance.Effort) == "" ||
			strings.TrimSpace(provenance.Source) == "" ||
			strings.TrimSpace(provenance.AgentsBlockVersion) == "" {
			return liveEvalCorpus{}, fmt.Errorf("case %q has incomplete provenance", item.ID)
		}
	}
	return corpus, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing corpus data: %w", err)
	}
	return errors.New("corpus contains trailing JSON values")
}

func runLiveEval(ctx context.Context, corpus liveEvalCorpus, classifier *Classifier) (liveEvalReport, error) {
	if classifier == nil {
		return liveEvalReport{}, errors.New("classifier is required")
	}
	actual := make(map[string]state.TaskStatus, len(corpus.Cases))
	unresolved := make([]TaskEvidence, 0, len(corpus.Cases))
	provenance := make(map[string]liveEvalProvenanceScore)
	deterministic := 0
	for _, item := range corpus.Cases {
		key := liveEvalProvenanceKey(item.Provenance)
		group := provenance[key]
		group.Total++
		facts := item.Facts.productionFacts()
		footer := ParseFooter(facts.Footer)
		if footer.Accepted {
			group.FooterAccepted++
		} else {
			group.FooterRejected++
		}
		resolution := Resolve(facts)
		if resolution.Resolved {
			actual[item.ID] = resolution.Status
			deterministic++
			group.Deterministic++
		} else {
			unresolved = append(unresolved, TaskEvidence{
				TaskID: item.ID, Revision: liveEvalRevision, Latest: *item.Latest, Previous: item.Previous,
			})
			group.ClassifierOwned++
		}
		provenance[key] = group
	}
	if len(unresolved) == 0 {
		return liveEvalReport{}, errors.New("corpus contains no classifier-owned cases")
	}
	for _, evidence := range unresolved {
		classifications := classifier.Classify(ctx, []TaskEvidence{evidence})
		if len(classifications) != 1 {
			return liveEvalReport{}, fmt.Errorf("classifier case %q produced %d results", evidence.TaskID, len(classifications))
		}
		classification := classifications[0]
		if classification.Diagnostic != nil {
			return liveEvalReport{}, fmt.Errorf(
				"classifier case %q failed: %s (%s; offending_item=%q)",
				classification.TaskID,
				classification.Diagnostic.Code,
				classification.Diagnostic.Message,
				classification.Diagnostic.OffendingItem,
			)
		}
		actual[classification.TaskID] = classification.Status
	}
	for _, item := range corpus.Cases {
		if _, ok := actual[item.ID]; !ok {
			return liveEvalReport{}, fmt.Errorf("case %q produced no classification", item.ID)
		}
	}
	report := scoreLiveEval(corpus.Cases, actual)
	report.Deterministic = deterministic
	report.Classified = len(unresolved)
	report.ByProvenance = provenance
	return report, nil
}

func scoreLiveEval(cases []liveEvalCase, actual map[string]state.TaskStatus) liveEvalReport {
	report := liveEvalReport{
		SchemaRevision: liveEvalSchemaRevision,
		Total:          len(cases),
		ByState:        make(map[state.TaskStatus]liveEvalStateScore),
		ByProvenance:   make(map[string]liveEvalProvenanceScore),
	}
	for _, item := range cases {
		got := actual[item.ID]
		score := report.ByState[item.Expected]
		score.Total++
		if got == item.Expected {
			score.Correct++
			report.Correct++
		} else {
			report.Errors = append(report.Errors, liveEvalError{ID: item.ID, Expected: item.Expected, Actual: got})
		}
		report.ByState[item.Expected] = score
		if got == state.StatusComplete && item.Expected != state.StatusComplete {
			report.FalseComplete++
		}
		if got == state.StatusNextSteps && item.Expected != state.StatusNextSteps {
			report.FalseNextSteps++
		}
	}
	sort.Slice(report.Errors, func(left, right int) bool { return report.Errors[left].ID < report.Errors[right].ID })
	return report
}

func liveEvalProvenanceKey(value liveEvalProvenance) string {
	return value.Model + "/" + value.Effort + "/" + value.Source + "/" + value.AgentsBlockVersion
}
