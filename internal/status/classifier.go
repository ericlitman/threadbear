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

func (c *Classifier) Classify(ctx context.Context, tasks []TaskEvidence) []Classification {
	results := make([]Classification, len(tasks))
	for index, task := range tasks {
		results[index] = unknownClassification(task, "unclassified", "classification did not produce a result", ClassificationMetadata{}, "")
	}
	if len(tasks) == 0 {
		return results
	}
	if c.config.ContextBudgetBytes <= 0 {
		return replaceUnknown(results, tasks, "invalid_context_budget", "advertised context budget is absent or invalid")
	}
	model := strings.TrimSpace(c.config.Model)
	effort := strings.TrimSpace(c.config.Effort)
	if model == "" || effort == "" || model != c.config.Model || effort != c.config.Effort {
		return replaceUnknown(results, tasks, "invalid_classifier_config", "classifier model and effort are required without surrounding whitespace")
	}
	if err := validateTaskEvidence(tasks); err != nil {
		return replaceUnknown(results, tasks, "invalid_task_evidence", "classifier task evidence is invalid")
	}

	first, oversized, err := PackTasks(tasks, c.config.ContextBudgetBytes, false)
	if err != nil {
		return replaceUnknown(results, tasks, "packing_failed", "classifier payload packing failed")
	}
	byID := make(map[string]Classification, len(tasks))
	for _, item := range oversized {
		byID[item.Task.TaskID] = unknownClassification(item.Task, "task_exceeds_context_budget", "complete task evidence exceeds the advertised context budget", ClassificationMetadata{}, "")
	}
	requested := make([]TaskEvidence, 0)
	requestedMetadata := make(map[string]ClassificationMetadata)
	for _, batch := range first {
		outcomes, diagnostic := c.runBatch(ctx, batch)
		if diagnostic != nil {
			for _, task := range batch.Tasks {
				byID[task.TaskID] = unknownClassification(task, diagnostic.Code, diagnostic.Message, diagnostic.Metadata, diagnostic.OffendingItem)
			}
			continue
		}
		for _, task := range batch.Tasks {
			outcome := outcomes[task.TaskID]
			if outcome.item.RequestPrevious {
				if task.Previous == nil {
					byID[task.TaskID] = unknownClassification(task, "previous_evidence_unavailable", "classifier requested unavailable previous-turn evidence", outcome.metadata, "")
					continue
				}
				requested = append(requested, task)
				requestedMetadata[task.TaskID] = outcome.metadata
				continue
			}
			byID[task.TaskID] = acceptedClassification(task, outcome)
		}
	}

	if len(requested) > 0 {
		second, secondOversized, packErr := PackTasks(requested, c.config.ContextBudgetBytes, true)
		if packErr != nil {
			for _, task := range requested {
				byID[task.TaskID] = unknownClassification(task, "packing_failed", "previous-turn payload packing failed", requestedMetadata[task.TaskID], "")
			}
		} else {
			for _, item := range secondOversized {
				byID[item.Task.TaskID] = unknownClassification(item.Task, "task_exceeds_context_budget", "complete expanded task evidence exceeds the advertised context budget", requestedMetadata[item.Task.TaskID], "")
			}
			for _, batch := range second {
				outcomes, diagnostic := c.runBatch(ctx, batch)
				if diagnostic != nil {
					for _, task := range batch.Tasks {
						metadata := mergeMetadata(requestedMetadata[task.TaskID], diagnostic.Metadata)
						byID[task.TaskID] = unknownClassification(task, diagnostic.Code, diagnostic.Message, metadata, diagnostic.OffendingItem)
					}
					continue
				}
				for _, task := range batch.Tasks {
					outcome := outcomes[task.TaskID]
					outcome.metadata = mergeMetadata(requestedMetadata[task.TaskID], outcome.metadata)
					if outcome.item.RequestPrevious {
						byID[task.TaskID] = unknownClassification(task, "previous_expansion_exhausted", "classifier requested previous-turn evidence after the single allowed expansion", outcome.metadata, "")
						continue
					}
					byID[task.TaskID] = acceptedClassification(task, outcome)
				}
			}
		}
	}

	for index, task := range tasks {
		if result, ok := byID[task.TaskID]; ok {
			results[index] = result
		}
	}
	return results
}

func (c *Classifier) runBatch(ctx context.Context, batch PackedBatch) (map[string]passOutcome, *batchDiagnostic) {
	result, err := c.runner.RunEphemeral(ctx, appserver.EphemeralRequest{
		Model:        c.config.Model,
		Effort:       c.config.Effort,
		Input:        batch.Input,
		OutputSchema: c.schema,
	})
	if err != nil {
		code := "ephemeral_call_failed"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			code = "ephemeral_call_timeout"
		}
		return nil, &batchDiagnostic{Code: code, Message: "ephemeral classifier call failed"}
	}
	metadata := ClassificationMetadata{UnprovenToolSources: normalizedSources(result.ToolRestriction.UnprovenToolSources)}
	text, itemDiagnostic := finalAssistantText(result.Turn.Items)
	if itemDiagnostic != nil {
		itemDiagnostic.Metadata = metadata
		return nil, itemDiagnostic
	}
	if !validToolRestrictions(result.ToolRestriction) {
		return nil, &batchDiagnostic{Code: "tool_controls_unconfirmed", Message: "ephemeral classifier controls were not fully confirmed", Metadata: metadata}
	}
	if result.Turn.Status != "completed" {
		return nil, &batchDiagnostic{Code: "classifier_turn_incomplete", Message: "ephemeral classifier turn did not complete", Metadata: metadata}
	}
	response, decodeErr := decodeClassifierResponse(text)
	if decodeErr != nil {
		return nil, &batchDiagnostic{Code: "malformed_classifier_response", Message: "classifier response did not match the strict schema", Metadata: metadata}
	}
	outcomes, validationErr := validateClassifierResponse(response, batch.Tasks, metadata)
	if validationErr != nil {
		return nil, &batchDiagnostic{Code: validationErr.code, Message: validationErr.message, Metadata: metadata}
	}
	return outcomes, nil
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
		case "reasoning":
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
		return "", &batchDiagnostic{Code: "unexpected_output_item", Message: "classifier batch contained no final assistant item"}
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

func validateClassifierResponse(response classifierResponse, tasks []TaskEvidence, metadata ClassificationMetadata) (map[string]passOutcome, *responseValidationError) {
	if response.SchemaRevision != SchemaRevision {
		return nil, &responseValidationError{code: "schema_revision_mismatch", message: "classifier response schema revision did not match the request"}
	}
	expected := make(map[string]TaskEvidence, len(tasks))
	for _, task := range tasks {
		expected[task.TaskID] = task
	}
	if len(response.Results) != len(expected) {
		return nil, &responseValidationError{code: "response_id_mismatch", message: "classifier response did not contain exactly the requested task IDs"}
	}
	outcomes := make(map[string]passOutcome, len(response.Results))
	for _, item := range response.Results {
		task, ok := expected[item.TaskID]
		if !ok {
			return nil, &responseValidationError{code: "response_id_mismatch", message: "classifier response contained an unexpected task ID"}
		}
		if _, duplicate := outcomes[item.TaskID]; duplicate {
			return nil, &responseValidationError{code: "response_id_mismatch", message: "classifier response contained a duplicate task ID"}
		}
		if item.TaskRevision != task.Revision {
			return nil, &responseValidationError{code: "response_revision_mismatch", message: "classifier response revision did not match captured evidence"}
		}
		if err := validateWireItem(item); err != nil {
			return nil, &responseValidationError{code: "invalid_response_fields", message: "classifier response contained an invalid state or field combination"}
		}
		outcomes[item.TaskID] = passOutcome{item: item, metadata: metadata}
	}
	return outcomes, nil
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
	return restriction.EnvironmentsDisabled && restriction.DynamicToolsDisabled && restriction.ApprovalsDisabled && restriction.ReadOnlySandbox && restriction.OutputConstrained
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
