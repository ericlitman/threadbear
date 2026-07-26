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

type liveEvalDiagnostic struct {
	ID      string `json:"id"`
	Code    string `json:"code"`
	Message string `json:"message"`
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
	Unscored       int                                     `json:"unscored"`
	FalseComplete  int                                     `json:"false_complete"`
	FalseNextSteps int                                     `json:"false_next_steps"`
	ByState        map[state.TaskStatus]liveEvalStateScore `json:"by_state"`
	ByProvenance   map[string]liveEvalProvenanceScore      `json:"by_provenance"`
	Errors         []liveEvalError                         `json:"errors"`
	Diagnostics    []liveEvalDiagnostic                    `json:"diagnostics,omitempty"`
}

// liveEvalSeries is the k-of-n release gate (Verification Contract amendment,
// 2026-07-26): the classifier path exposes no sampling controls, so a single
// run is a coin flip in both directions. The gate therefore fails only on
// SYSTEMATIC dangerous errors - a case wrong in a dangerous direction in a
// majority of runs - and reports the rest as flap rate rather than pretending
// the variance is zero.
type liveEvalSeries struct {
	SchemaRevision string               `json:"schema_revision"`
	Runs           int                  `json:"runs"`
	Threshold      int                  `json:"threshold"`
	Caveat         string               `json:"caveat"`
	RunSummaries   []liveEvalRunSummary `json:"run_summaries"`
	Systematic     []liveEvalCaseSeries `json:"systematic"`
	Flapping       []liveEvalCaseSeries `json:"flapping"`
	Unscoreable    []liveEvalCaseSeries `json:"unscoreable"`
	Reports        []liveEvalReport     `json:"reports"`
}

type liveEvalRunSummary struct {
	Correct        int `json:"correct"`
	FalseComplete  int `json:"false_complete"`
	FalseNextSteps int `json:"false_next_steps"`
	Unscored       int `json:"unscored"`
}

type liveEvalCaseSeries struct {
	ID         string             `json:"id"`
	Expected   state.TaskStatus   `json:"expected"`
	Dangerous  int                `json:"dangerous_runs"`
	Diagnosed  int                `json:"diagnosed_runs"`
	Actuals    []state.TaskStatus `json:"actuals"`
	Directions []string           `json:"directions"`
}

// The single-case-per-call caveat is a condition of the BEAR-28 acceptance:
// production packs batches (KTD3), the eval does not, so a green gate is a
// floor rather than a production guarantee and must say so in its own output.
const liveEvalFloorCaveat = "cases are classified one per call; production packs batches (KTD3), so this gate is a floor, not a production guarantee"

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
		if item.Facts.Footer.Message != item.Latest.FinalAgent {
			return liveEvalCorpus{}, fmt.Errorf("case %q facts.footer.message must equal latest.final_agent", item.ID)
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
	// A diagnostic on one case is data about that case, not grounds to discard
	// the run: aborting here made an hour of classification unreproducible over
	// a single previous_evidence_unavailable (BEAR-29). Diagnosed cases are
	// recorded, excluded from accuracy denominators, and surface in the series
	// aggregation as unscoreable when they persist.
	diagnostics := make([]liveEvalDiagnostic, 0)
	for _, evidence := range unresolved {
		classifications := classifier.Classify(ctx, []TaskEvidence{evidence})
		if len(classifications) != 1 {
			return liveEvalReport{}, fmt.Errorf("classifier case %q produced %d results", evidence.TaskID, len(classifications))
		}
		classification := classifications[0]
		if classification.Diagnostic != nil {
			// previous_evidence_unavailable is a terminal production disposition,
			// not an infrastructure failure: the classifier returns status
			// unknown for it (classifier.go), and the heartbeat records exactly
			// that. Scoring it as unknown is the faithful treatment; only
			// transient codes (turn incomplete, call failed) stay unscored.
			if classification.Diagnostic.Code == "previous_evidence_unavailable" {
				actual[classification.TaskID] = classification.Status
				continue
			}
			diagnostics = append(diagnostics, liveEvalDiagnostic{
				ID:      evidence.TaskID,
				Code:    classification.Diagnostic.Code,
				Message: classification.Diagnostic.Message,
			})
			continue
		}
		actual[classification.TaskID] = classification.Status
	}
	diagnosed := make(map[string]bool, len(diagnostics))
	for _, diagnostic := range diagnostics {
		diagnosed[diagnostic.ID] = true
	}
	for _, item := range corpus.Cases {
		if _, ok := actual[item.ID]; !ok && !diagnosed[item.ID] {
			return liveEvalReport{}, fmt.Errorf("case %q produced no classification", item.ID)
		}
	}
	report := scoreLiveEval(corpus.Cases, actual, diagnosed)
	report.Deterministic = deterministic
	report.Classified = len(unresolved)
	report.ByProvenance = provenance
	report.Diagnostics = diagnostics
	return report, nil
}

func aggregateLiveEvalSeries(cases []liveEvalCase, reports []liveEvalReport) liveEvalSeries {
	series := liveEvalSeries{
		SchemaRevision: liveEvalSchemaRevision,
		Runs:           len(reports),
		// Three-quarters, not a bare majority (amended 2026-07-26, operator-
		// approved): genuinely borderline cases run at ~60-85% correct and a
		// majority threshold gates on their coin flips - every case that hit
		// exactly 3-of-5 proved to be a label defect, while every true model
		// defect ran at 4-of-5 or worse. For five runs this is 4.
		Threshold:      (3*len(reports) + 3) / 4,
		Caveat:         liveEvalFloorCaveat,
		Reports:        reports,
	}
	expected := make(map[string]state.TaskStatus, len(cases))
	for _, item := range cases {
		expected[item.ID] = item.Expected
	}
	wrong := make(map[string][]state.TaskStatus)
	diagnosedRuns := make(map[string]int)
	for _, report := range reports {
		series.RunSummaries = append(series.RunSummaries, liveEvalRunSummary{
			Correct:        report.Correct,
			FalseComplete:  report.FalseComplete,
			FalseNextSteps: report.FalseNextSteps,
			Unscored:       report.Unscored,
		})
		for _, failure := range report.Errors {
			wrong[failure.ID] = append(wrong[failure.ID], failure.Actual)
		}
		for _, diagnostic := range report.Diagnostics {
			diagnosedRuns[diagnostic.ID]++
		}
	}
	for id, actuals := range wrong {
		entry := liveEvalCaseSeries{ID: id, Expected: expected[id], Actuals: actuals}
		directions := make(map[string]bool)
		for _, actual := range actuals {
			if actual == state.StatusComplete && expected[id] != state.StatusComplete {
				entry.Dangerous++
				directions["false_complete"] = true
			}
			if actual == state.StatusNextSteps && expected[id] != state.StatusNextSteps {
				entry.Dangerous++
				directions["false_next_steps"] = true
			}
		}
		for direction := range directions {
			entry.Directions = append(entry.Directions, direction)
		}
		sort.Strings(entry.Directions)
		if entry.Dangerous >= series.Threshold {
			series.Systematic = append(series.Systematic, entry)
		} else if entry.Dangerous > 0 {
			series.Flapping = append(series.Flapping, entry)
		}
	}
	for id, count := range diagnosedRuns {
		if count >= series.Threshold {
			series.Unscoreable = append(series.Unscoreable, liveEvalCaseSeries{ID: id, Expected: expected[id], Diagnosed: count})
		}
	}
	sort.Slice(series.Systematic, func(left, right int) bool { return series.Systematic[left].ID < series.Systematic[right].ID })
	sort.Slice(series.Flapping, func(left, right int) bool { return series.Flapping[left].ID < series.Flapping[right].ID })
	sort.Slice(series.Unscoreable, func(left, right int) bool { return series.Unscoreable[left].ID < series.Unscoreable[right].ID })
	return series
}

func scoreLiveEval(cases []liveEvalCase, actual map[string]state.TaskStatus, diagnosed map[string]bool) liveEvalReport {
	report := liveEvalReport{
		SchemaRevision: liveEvalSchemaRevision,
		Total:          len(cases),
		ByState:        make(map[state.TaskStatus]liveEvalStateScore),
		ByProvenance:   make(map[string]liveEvalProvenanceScore),
	}
	for _, item := range cases {
		// A diagnosed case was never classified; scoring it as wrong would
		// punish harness conditions, and scoring it as right would hide them.
		if diagnosed[item.ID] {
			report.Unscored++
			continue
		}
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
