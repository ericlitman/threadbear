package status

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ericlitman/threadbear/internal/codex/appserver"
	"github.com/ericlitman/threadbear/internal/state"
)

//go:embed schema.json
var classifierSchemaBytes []byte

type EphemeralRunner interface {
	RunEphemeral(context.Context, appserver.EphemeralRequest) (appserver.EphemeralResult, error)
}

type ClassifierConfig struct {
	Model              string
	Effort             string
	ContextBudgetBytes int
	MaxParallelBatches int
}

type Classifier struct {
	runner EphemeralRunner
	config ClassifierConfig
	schema map[string]any
}

type ClassificationMetadata struct {
	UnprovenToolSources []string `json:"unproven_tool_sources,omitempty"`
}

type ClassificationDiagnostic struct {
	Code          string `json:"code"`
	Message       string `json:"message"`
	OffendingItem string `json:"offending_item,omitempty"`
}

type PreviousEvidenceResult struct {
	TaskID    string
	Revision  string
	Evidence  *TurnEvidence
	ErrorCode string
}

type PreviousEvidenceLoader func(context.Context, []TaskEvidence) []PreviousEvidenceResult

type Classification struct {
	TaskID         string                    `json:"task_id"`
	Revision       string                    `json:"revision"`
	Status         state.TaskStatus          `json:"status"`
	Provenance     state.Provenance          `json:"provenance"`
	DurableSubject string                    `json:"durable_subject,omitempty"`
	ManagedAction  string                    `json:"managed_action,omitempty"`
	Diagnostic     *ClassificationDiagnostic `json:"diagnostic,omitempty"`
	Metadata       ClassificationMetadata    `json:"metadata,omitempty"`
}

type classifierResponse struct {
	SchemaRevision string               `json:"schema_revision"`
	Results        []classifierWireItem `json:"results"`
}

type classifierWireItem struct {
	TaskID          string           `json:"task_id"`
	TaskRevision    string           `json:"task_revision"`
	State           state.TaskStatus `json:"state"`
	DurableSubject  string           `json:"durable_subject"`
	ManagedAction   string           `json:"managed_action"`
	RequestPrevious bool             `json:"request_previous"`
}

func (i *classifierWireItem) UnmarshalJSON(data []byte) error {
	var wire struct {
		TaskID          *string           `json:"task_id"`
		TaskRevision    *string           `json:"task_revision"`
		State           *state.TaskStatus `json:"state"`
		DurableSubject  *string           `json:"durable_subject"`
		ManagedAction   *string           `json:"managed_action"`
		RequestPrevious *bool             `json:"request_previous"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return err
	}
	if wire.TaskID == nil || wire.TaskRevision == nil || wire.State == nil || wire.DurableSubject == nil || wire.ManagedAction == nil || wire.RequestPrevious == nil {
		return errors.New("classifier result is missing a required field")
	}
	*i = classifierWireItem{TaskID: *wire.TaskID, TaskRevision: *wire.TaskRevision, State: *wire.State, DurableSubject: *wire.DurableSubject, ManagedAction: *wire.ManagedAction, RequestPrevious: *wire.RequestPrevious}
	return nil
}

type passOutcome struct {
	item     classifierWireItem
	metadata ClassificationMetadata
}

func NewClassifier(runner EphemeralRunner, config ClassifierConfig) (*Classifier, error) {
	if runner == nil {
		return nil, errors.New("ephemeral runner is required")
	}
	var schema map[string]any
	if err := json.Unmarshal(classifierSchemaBytes, &schema); err != nil {
		return nil, fmt.Errorf("decode classifier schema: %w", err)
	}
	return &Classifier{runner: runner, config: config, schema: schema}, nil
}

type ClassificationPass string

const (
	ClassificationPassFirst    ClassificationPass = "first"
	ClassificationPassPrevious ClassificationPass = "previous"
)

type TaskKey struct {
	TaskID   string
	Revision string
}

type ClassificationResume struct {
	PreviousRequested map[string]string
}

type ClassificationBatchEvent struct {
	Pass              ClassificationPass
	Total             int
	Completed         int
	Duration          time.Duration
	Classifications   []Classification
	PreviousRequested []TaskKey
}

type ClassificationObserver func(ClassificationBatchEvent) error

func (c *Classifier) Classify(ctx context.Context, tasks []TaskEvidence) []Classification {
	return c.classifyLegacy(ctx, tasks, nil)
}

func (c *Classifier) ClassifyWithPrevious(ctx context.Context, tasks []TaskEvidence, load PreviousEvidenceLoader) []Classification {
	return c.classifyLegacy(ctx, tasks, load)
}

func (c *Classifier) classifyLegacy(ctx context.Context, tasks []TaskEvidence, load PreviousEvidenceLoader) []Classification {
	results, _ := c.ClassifyWithProgress(ctx, tasks, load, ClassificationResume{}, nil)
	return results
}

func (c *Classifier) ClassifyWithProgress(ctx context.Context, tasks []TaskEvidence, load PreviousEvidenceLoader, resume ClassificationResume, observer ClassificationObserver) ([]Classification, error) {
	results := make([]Classification, len(tasks))
	for index, task := range tasks {
		results[index] = unknownClassification(task, "unclassified", "classification did not produce a result", ClassificationMetadata{}, "")
	}
	if len(tasks) == 0 {
		return results, nil
	}
	if c.config.ContextBudgetBytes <= 0 {
		return replaceUnknown(results, tasks, "invalid_context_budget", "advertised context budget is absent or invalid"), nil
	}
	model := strings.TrimSpace(c.config.Model)
	effort := strings.TrimSpace(c.config.Effort)
	if model == "" || effort == "" || model != c.config.Model || effort != c.config.Effort {
		return replaceUnknown(results, tasks, "invalid_classifier_config", "classifier model and effort are required without surrounding whitespace"), nil
	}
	if err := validateTaskEvidence(tasks); err != nil {
		return replaceUnknown(results, tasks, "invalid_task_evidence", "classifier task evidence is invalid"), nil
	}

	byID := make(map[string]Classification, len(tasks))
	firstTasks := make([]TaskEvidence, 0, len(tasks))
	requested := make([]TaskEvidence, 0)
	requestedMetadata := make(map[string]ClassificationMetadata)
	for _, task := range tasks {
		if resume.PreviousRequested != nil && resume.PreviousRequested[task.TaskID] == task.Revision {
			requested = append(requested, task)
			continue
		}
		firstTasks = append(firstTasks, task)
	}
	first, oversized, err := PackTasks(firstTasks, c.config.ContextBudgetBytes, false)
	if err != nil {
		return replaceUnknown(results, tasks, "packing_failed", "classifier payload packing failed"), nil
	}
	for _, item := range oversized {
		byID[item.Task.TaskID] = unknownClassification(item.Task, "task_exceeds_context_budget", "complete task evidence exceeds the advertised context budget", ClassificationMetadata{}, "")
	}
	if observer != nil {
		if err := observer(ClassificationBatchEvent{Pass: ClassificationPassFirst, Total: len(first)}); err != nil {
			return results, err
		}
	}
	firstCompleted := 0
	err = c.executeBatches(ctx, first, func(execution batchExecution) error {
		firstCompleted++
		classifications := make([]Classification, 0, len(execution.batch.Tasks))
		previous := make([]TaskKey, 0)
		if execution.diagnostic != nil {
			for _, task := range execution.batch.Tasks {
				classification := unknownClassification(task, execution.diagnostic.Code, execution.diagnostic.Message, execution.diagnostic.Metadata, execution.diagnostic.OffendingItem)
				byID[task.TaskID] = classification
				classifications = append(classifications, classification)
			}
		} else {
			for _, task := range execution.batch.Tasks {
				outcome, classified := execution.outcomes[task.TaskID]
				if !classified {
					rowDiagnostic := execution.rowDiagnostics[task.TaskID]
					if rowDiagnostic == nil {
						rowDiagnostic = &batchDiagnostic{Code: "missing_from_response", Message: "classifier response contained no valid row for this task"}
					}
					classification := unknownClassification(task, rowDiagnostic.Code, rowDiagnostic.Message, rowDiagnostic.Metadata, rowDiagnostic.OffendingItem)
					byID[task.TaskID] = classification
					classifications = append(classifications, classification)
					continue
				}
				if outcome.item.RequestPrevious {
					requested = append(requested, task)
					requestedMetadata[task.TaskID] = outcome.metadata
					previous = append(previous, TaskKey{TaskID: task.TaskID, Revision: task.Revision})
					continue
				}
				classification := acceptedClassification(task, outcome)
				byID[task.TaskID] = classification
				classifications = append(classifications, classification)
			}
		}
		if observer != nil {
			return observer(ClassificationBatchEvent{Pass: ClassificationPassFirst, Total: len(first), Completed: firstCompleted, Duration: execution.duration, Classifications: classifications, PreviousRequested: previous})
		}
		return nil
	})
	if err != nil {
		return results, err
	}

	if len(requested) > 0 {
		missing := make([]TaskEvidence, 0)
		for _, task := range requested {
			if task.Previous == nil {
				missing = append(missing, task)
			}
		}
		if len(missing) > 0 && load != nil {
			loaded := load(ctx, missing)
			byLoadedID := make(map[string]PreviousEvidenceResult, len(loaded))
			for _, item := range loaded {
				if _, duplicate := byLoadedID[item.TaskID]; !duplicate {
					byLoadedID[item.TaskID] = item
				}
			}
			for index := range requested {
				if requested[index].Previous != nil {
					continue
				}
				item, ok := byLoadedID[requested[index].TaskID]
				if !ok || item.Revision != requested[index].Revision || item.Evidence == nil {
					code := "previous_evidence_unavailable"
					if ok && item.ErrorCode != "" {
						code = item.ErrorCode
					}
					byID[requested[index].TaskID] = unknownClassification(requested[index], code, "classifier requested unavailable previous-turn evidence", requestedMetadata[requested[index].TaskID], "")
					continue
				}
				previous := *item.Evidence
				requested[index].Previous = &previous
			}
		}
		ready := requested[:0]
		for _, task := range requested {
			if task.Previous == nil {
				if _, set := byID[task.TaskID]; !set {
					byID[task.TaskID] = unknownClassification(task, "previous_evidence_unavailable", "classifier requested unavailable previous-turn evidence", requestedMetadata[task.TaskID], "")
				}
				continue
			}
			ready = append(ready, task)
		}
		requested = ready
		second, secondOversized, packErr := PackTasks(requested, c.config.ContextBudgetBytes, true)
		if packErr != nil {
			for _, task := range requested {
				byID[task.TaskID] = unknownClassification(task, "packing_failed", "previous-turn payload packing failed", requestedMetadata[task.TaskID], "")
			}
		} else {
			for _, item := range secondOversized {
				byID[item.Task.TaskID] = unknownClassification(item.Task, "task_exceeds_context_budget", "complete expanded task evidence exceeds the advertised context budget", requestedMetadata[item.Task.TaskID], "")
			}
			if observer != nil {
				if err := observer(ClassificationBatchEvent{Pass: ClassificationPassPrevious, Total: len(second)}); err != nil {
					return results, err
				}
			}
			secondCompleted := 0
			err = c.executeBatches(ctx, second, func(execution batchExecution) error {
				secondCompleted++
				classifications := make([]Classification, 0, len(execution.batch.Tasks))
				for _, task := range execution.batch.Tasks {
					if execution.diagnostic != nil {
						metadata := mergeMetadata(requestedMetadata[task.TaskID], execution.diagnostic.Metadata)
						classification := unknownClassification(task, execution.diagnostic.Code, execution.diagnostic.Message, metadata, execution.diagnostic.OffendingItem)
						byID[task.TaskID] = classification
						classifications = append(classifications, classification)
						continue
					}
					outcome, classified := execution.outcomes[task.TaskID]
					if !classified {
						rowDiagnostic := execution.rowDiagnostics[task.TaskID]
						if rowDiagnostic == nil {
							rowDiagnostic = &batchDiagnostic{Code: "missing_from_response", Message: "classifier response contained no valid row for this task"}
						}
						metadata := mergeMetadata(requestedMetadata[task.TaskID], rowDiagnostic.Metadata)
						classification := unknownClassification(task, rowDiagnostic.Code, rowDiagnostic.Message, metadata, rowDiagnostic.OffendingItem)
						byID[task.TaskID] = classification
						classifications = append(classifications, classification)
						continue
					}
					outcome.metadata = mergeMetadata(requestedMetadata[task.TaskID], outcome.metadata)
					var classification Classification
					if outcome.item.RequestPrevious {
						classification = unknownClassification(task, "previous_expansion_exhausted", "classifier requested previous-turn evidence after the single allowed expansion", outcome.metadata, "")
					} else {
						classification = acceptedClassification(task, outcome)
					}
					byID[task.TaskID] = classification
					classifications = append(classifications, classification)
				}
				if observer != nil {
					return observer(ClassificationBatchEvent{Pass: ClassificationPassPrevious, Total: len(second), Completed: secondCompleted, Duration: execution.duration, Classifications: classifications})
				}
				return nil
			})
			if err != nil {
				return results, err
			}
		}
	}

	for index, task := range tasks {
		if result, ok := byID[task.TaskID]; ok {
			results[index] = result
		}
	}
	return results, nil
}

type batchExecution struct {
	batch          PackedBatch
	outcomes       map[string]passOutcome
	rowDiagnostics map[string]*batchDiagnostic
	diagnostic     *batchDiagnostic
	duration       time.Duration
	err            error
}

func (c *Classifier) executeBatches(ctx context.Context, batches []PackedBatch, handle func(batchExecution) error) error {
	if len(batches) == 0 {
		return nil
	}
	parallel := c.config.MaxParallelBatches
	if parallel <= 0 {
		parallel = 1
	}
	if parallel > len(batches) {
		parallel = len(batches)
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan PackedBatch)
	completed := make(chan batchExecution)
	var workers sync.WaitGroup
	for index := 0; index < parallel; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for batch := range jobs {
				if workCtx.Err() != nil {
					return
				}
				started := time.Now()
				outcomes, rowDiagnostics, diagnostic := c.runBatch(workCtx, batch)
				execution := batchExecution{batch: batch, outcomes: outcomes, rowDiagnostics: rowDiagnostics, diagnostic: diagnostic, duration: time.Since(started)}
				if workCtx.Err() != nil {
					execution.err = workCtx.Err()
				}
				select {
				case completed <- execution:
				case <-workCtx.Done():
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, batch := range batches {
			select {
			case jobs <- batch:
			case <-workCtx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(completed)
	}()
	var firstErr error
	for execution := range completed {
		if execution.err != nil && firstErr == nil {
			firstErr = execution.err
			cancel()
			continue
		}
		if firstErr == nil {
			if err := handle(execution); err != nil {
				firstErr = err
				cancel()
			}
		}
	}
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

func (c *Classifier) runBatch(ctx context.Context, batch PackedBatch) (map[string]passOutcome, map[string]*batchDiagnostic, *batchDiagnostic) {
	result, err := c.runner.RunEphemeral(ctx, appserver.EphemeralRequest{
		Model:             c.config.Model,
		Effort:            c.config.Effort,
		Input:             batch.Input,
		OutputSchema:      c.schema,
		ToolConfig:        appserver.ClassifierToolConfig(),
		PermissionProfile: ":read-only",
	})
	if err != nil {
		code := "ephemeral_call_failed"
		lowerError := strings.ToLower(err.Error())
		if strings.Contains(lowerError, "rate limit") || strings.Contains(lowerError, "429") {
			code = "ephemeral_rate_limited"
		} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			code = "ephemeral_call_timeout"
		}
		return nil, nil, &batchDiagnostic{Code: code, Message: "ephemeral classifier call failed"}
	}
	metadata := ClassificationMetadata{UnprovenToolSources: normalizedSources(result.ToolRestriction.UnprovenToolSources)}
	if result.Turn.Status != "completed" {
		if _, diagnostic := finalAssistantText(result.Turn.Items); diagnostic != nil && diagnostic.OffendingItem != "" {
			diagnostic.Metadata = metadata
			return nil, nil, diagnostic
		}
		return nil, nil, &batchDiagnostic{Code: "classifier_turn_incomplete", Message: "ephemeral classifier turn did not complete", Metadata: metadata}
	}
	text, itemDiagnostic := finalAssistantText(result.Turn.Items)
	if itemDiagnostic != nil {
		itemDiagnostic.Metadata = metadata
		return nil, nil, itemDiagnostic
	}
	if !validToolRestrictions(result.ToolRestriction) {
		return nil, nil, &batchDiagnostic{Code: "tool_controls_unconfirmed", Message: "ephemeral classifier controls were not fully confirmed", Metadata: metadata}
	}
	response, decodeErr := decodeClassifierResponse(text)
	if decodeErr != nil {
		return nil, nil, &batchDiagnostic{Code: "malformed_classifier_response", Message: "classifier response did not match the strict schema", Metadata: metadata}
	}
	outcomes, rowFailures, validationErr := validateClassifierResponse(response, batch.Tasks, metadata)
	if validationErr != nil {
		return nil, nil, &batchDiagnostic{Code: validationErr.code, Message: validationErr.message, Metadata: metadata}
	}
	rowDiagnostics := make(map[string]*batchDiagnostic, len(rowFailures))
	for taskID, failure := range rowFailures {
		rowDiagnostics[taskID] = &batchDiagnostic{Code: failure.code, Message: failure.message, Metadata: metadata}
	}
	return outcomes, rowDiagnostics, nil
}

type batchDiagnostic struct {
	Code          string
	Message       string
	OffendingItem string
	Metadata      ClassificationMetadata
}

type responseValidationError struct {
	code    string
	message string
}

func finalAssistantText(items []appserver.TurnItem) (string, *batchDiagnostic) {
	var final *appserver.TurnItem
	for index := range items {
		item := &items[index]
		switch item.Type {
		case "reasoning", "userMessage", "user_message":
			continue
		case "agentMessage", "agent_message":
			if final != nil {
				return "", &batchDiagnostic{Code: "unexpected_output_item", Message: "classifier batch must contain exactly one final assistant item", OffendingItem: safeItemName(final.Type)}
			}
			if item.Phase != "final_answer" && item.Phase != "finalAnswer" {
				return "", &batchDiagnostic{Code: "unexpected_output_item", Message: "classifier assistant item was not final", OffendingItem: safeItemName(item.Type)}
			}
			final = item
		default:
			return "", &batchDiagnostic{Code: "unexpected_output_item", Message: "classifier batch contained a disallowed output item type", OffendingItem: safeItemName(item.Type)}
		}
	}
	if final == nil {
		types := make([]string, 0, len(items))
		for _, item := range items {
			types = append(types, safeItemName(item.Type))
		}
		message := "classifier batch contained no final assistant item"
		if len(types) > 0 {
			message += "; observed item types: " + strings.Join(types, ",")
		}
		return "", &batchDiagnostic{Code: "unexpected_output_item", Message: message}
	}
	text := final.Text
	if text != "" && len(final.Content) > 0 && !bytes.Equal(final.Content, []byte("null")) {
		return "", &batchDiagnostic{Code: "malformed_classifier_response", Message: "classifier final assistant item used ambiguous content fields", OffendingItem: safeItemName(final.Type)}
	}
	if text == "" {
		text = contentText(final.Content)
	}
	if strings.TrimSpace(text) == "" {
		return "", &batchDiagnostic{Code: "malformed_classifier_response", Message: "classifier final assistant item was empty", OffendingItem: safeItemName(final.Type)}
	}
	return text, nil
}

func contentText(content json.RawMessage) string {
	if len(content) == 0 || bytes.Equal(content, []byte("null")) {
		return ""
	}
	var direct string
	if json.Unmarshal(content, &direct) == nil {
		return direct
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &parts) != nil || len(parts) != 1 {
		return ""
	}
	if parts[0].Type != "outputText" && parts[0].Type != "output_text" && parts[0].Type != "text" {
		return ""
	}
	return parts[0].Text
}

func decodeClassifierResponse(text string) (classifierResponse, error) {
	var response classifierResponse
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return classifierResponse{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return classifierResponse{}, errors.New("multiple JSON values")
		}
		return classifierResponse{}, err
	}
	return response, nil
}

// validateClassifierResponse salvages every individually valid row instead of
// rejecting the whole batch on the first defect. The all-or-nothing rule met
// reality on 2026-07-26: a 71-task batch came back with one chimera ID (the
// prefix of one requested UUID fused onto the suffix of its neighbour) and one
// ID missing its final character - two bad rows - and validation discarded all
// 71 results, every heartbeat, forever. A model transcribing dozens of 36-char
// random strings will occasionally miscopy one; the response rows that do
// match are still exact matches and lose nothing by the neighbours' failure.
//
// Rows that match no requested ID are unattributable and dropped; requested
// tasks with no surviving row are reported per task so only they retry. A
// schema-revision mismatch still rejects the batch outright - that is not a
// row defect, the whole response answered a different contract.
func validateClassifierResponse(response classifierResponse, tasks []TaskEvidence, metadata ClassificationMetadata) (map[string]passOutcome, map[string]*responseValidationError, *responseValidationError) {
	if response.SchemaRevision != SchemaRevision {
		return nil, nil, &responseValidationError{code: "schema_revision_mismatch", message: "classifier response schema revision did not match the request"}
	}
	expected := make(map[string]TaskEvidence, len(tasks))
	for _, task := range tasks {
		expected[task.TaskID] = task
	}
	outcomes := make(map[string]passOutcome, len(response.Results))
	rowFailures := make(map[string]*responseValidationError)
	for _, item := range response.Results {
		task, ok := expected[item.TaskID]
		if !ok {
			// Unattributable: no requested task carries this ID, so there is
			// no task to attach a result or a diagnostic to. The real task the
			// model mangled surfaces below as missing.
			continue
		}
		if _, duplicate := outcomes[item.TaskID]; duplicate {
			// First valid occurrence wins; later duplicates are ignored.
			continue
		}
		if item.TaskRevision != task.Revision {
			rowFailures[item.TaskID] = &responseValidationError{code: "response_revision_mismatch", message: "classifier response revision did not match captured evidence"}
			continue
		}
		if err := validateWireItem(item); err != nil {
			rowFailures[item.TaskID] = &responseValidationError{code: "invalid_response_fields", message: "classifier response contained an invalid state or field combination"}
			continue
		}
		delete(rowFailures, item.TaskID)
		outcomes[item.TaskID] = passOutcome{item: item, metadata: metadata}
	}
	for _, task := range tasks {
		if _, ok := outcomes[task.TaskID]; ok {
			continue
		}
		if _, diagnosed := rowFailures[task.TaskID]; diagnosed {
			continue
		}
		rowFailures[task.TaskID] = &responseValidationError{code: "missing_from_response", message: "classifier response contained no valid row for this task"}
	}
	return outcomes, rowFailures, nil
}

func validateWireItem(item classifierWireItem) error {
	if !item.State.Valid() {
		return errors.New("invalid state")
	}
	subject := strings.TrimSpace(item.DurableSubject)
	action := strings.TrimSpace(item.ManagedAction)
	if subject != item.DurableSubject || action != item.ManagedAction {
		return errors.New("surrounding whitespace")
	}
	if item.RequestPrevious {
		if item.State != state.StatusUnknown || subject != "" || action != "" {
			return errors.New("invalid previous request")
		}
		return nil
	}
	if item.State != state.StatusUnknown && subject == "" {
		return errors.New("missing durable subject")
	}
	switch item.State {
	case state.StatusComplete, state.StatusUnknown:
		if action != "" {
			return errors.New("unexpected action")
		}
	case state.StatusBlocked, state.StatusNeedsInput, state.StatusNextSteps:
		if action == "" {
			return errors.New("missing action")
		}
	}
	return nil
}

func validToolRestrictions(restriction appserver.ToolRestriction) bool {
	return restriction.ConfigOverride && restriction.PermissionProfile && restriction.EnvironmentsDisabled && restriction.DynamicToolsDisabled && restriction.ApprovalsDisabled && restriction.OutputConstrained && len(restriction.UnprovenToolSources) == 0
}

func acceptedClassification(task TaskEvidence, outcome passOutcome) Classification {
	return Classification{TaskID: task.TaskID, Revision: task.Revision, Status: outcome.item.State, Provenance: state.ProvenanceLuna, DurableSubject: outcome.item.DurableSubject, ManagedAction: outcome.item.ManagedAction, Metadata: outcome.metadata}
}

func unknownClassification(task TaskEvidence, code, message string, metadata ClassificationMetadata, offendingItem string) Classification {
	return Classification{TaskID: task.TaskID, Revision: task.Revision, Status: state.StatusUnknown, Provenance: state.ProvenanceUnknown, Diagnostic: &ClassificationDiagnostic{Code: code, Message: message, OffendingItem: offendingItem}, Metadata: metadata}
}

func replaceUnknown(results []Classification, tasks []TaskEvidence, code, message string) []Classification {
	for index, task := range tasks {
		results[index] = unknownClassification(task, code, message, ClassificationMetadata{}, "")
	}
	return results
}

func mergeMetadata(left, right ClassificationMetadata) ClassificationMetadata {
	return ClassificationMetadata{UnprovenToolSources: normalizedSources(append(append([]string{}, left.UnprovenToolSources...), right.UnprovenToolSources...))}
}

func normalizedSources(sources []string) []string {
	unique := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if source != "" {
			unique[source] = struct{}{}
		}
	}
	result := make([]string, 0, len(unique))
	for source := range unique {
		result = append(result, source)
	}
	sort.Strings(result)
	return result
}

func safeItemName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "<empty>"
	}
	var output strings.Builder
	for _, char := range name {
		if char >= 0x20 && char <= 0x7e {
			output.WriteRune(char)
		}
		if output.Len() >= 80 {
			break
		}
	}
	if output.Len() == 0 {
		return "<non-printable>"
	}
	return output.String()
}

func (c Classification) StateResult() state.ClassificationResult {
	return state.ClassificationResult{TaskID: c.TaskID, Revision: c.Revision, Status: c.Status, Provenance: c.Provenance, DurableSubject: c.DurableSubject, ManagedAction: c.ManagedAction}
}
