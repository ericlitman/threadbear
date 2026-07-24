package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ericlitman/threadbear/internal/state"
)

const ThreadWatchSchemaVersion = 1

var ErrInvalidThreadWatchState = errors.New("invalid ThreadWatch state")

type Migration struct {
	ControlTaskID    string
	HeartbeatSeconds int
	State            state.State
}

type threadWatchState struct {
	SchemaVersion      *int                         `json:"schemaVersion"`
	ControllerThreadID *string                      `json:"controllerThreadId"`
	CycleCompletedAtMS *int64                       `json:"cycleCompletedAtMs"`
	RetryIDs           *[]string                    `json:"retryIds"`
	Threads            *map[string]threadWatchEntry `json:"threads"`
}

type threadWatchEntry struct {
	ActivityAtMS *int64  `json:"activityAtMs"`
	Title        *string `json:"title"`
}

func DecodeThreadWatch(data []byte) (Migration, error) {
	var legacy threadWatchState
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&legacy); err != nil {
		return Migration{}, fmt.Errorf("%w: %v", ErrInvalidThreadWatchState, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return Migration{}, fmt.Errorf("%w: %v", ErrInvalidThreadWatchState, err)
	}
	if legacy.SchemaVersion == nil || *legacy.SchemaVersion != ThreadWatchSchemaVersion {
		found := 0
		if legacy.SchemaVersion != nil {
			found = *legacy.SchemaVersion
		}
		return Migration{}, fmt.Errorf("%w: schemaVersion %d is unsupported", ErrInvalidThreadWatchState, found)
	}
	if legacy.ControllerThreadID == nil || !canonical(*legacy.ControllerThreadID) {
		return Migration{}, fmt.Errorf("%w: controllerThreadId is required", ErrInvalidThreadWatchState)
	}
	if legacy.CycleCompletedAtMS == nil || *legacy.CycleCompletedAtMS <= 0 || legacy.RetryIDs == nil || legacy.Threads == nil {
		return Migration{}, fmt.Errorf("%w: required field is missing", ErrInvalidThreadWatchState)
	}
	completedAt := time.UnixMilli(*legacy.CycleCompletedAtMS).UTC()
	migrated := state.New()
	migrated.LastCompletedHeartbeat = &completedAt
	for id, entry := range *legacy.Threads {
		if !canonical(id) || entry.ActivityAtMS == nil || *entry.ActivityAtMS <= 0 || entry.Title == nil {
			return Migration{}, fmt.Errorf("%w: thread %q is invalid", ErrInvalidThreadWatchState, id)
		}
		activity := time.UnixMilli(*entry.ActivityAtMS).UTC()
		migrated.Tasks[id] = state.TaskRecord{
			TaskID: id, CapturedRevision: "threadwatch:" + id, CapturedTitle: *entry.Title,
			Status: statusFromLegacyTitle(*entry.Title), Provenance: state.ProvenanceUnknown,
			StateStartedAt: activity, LastSubstantiveActivity: activity,
		}
	}
	seenRetries := make(map[string]struct{}, len(*legacy.RetryIDs))
	for _, id := range *legacy.RetryIDs {
		if !canonical(id) {
			return Migration{}, fmt.Errorf("%w: retry ID is invalid", ErrInvalidThreadWatchState)
		}
		if _, duplicate := seenRetries[id]; duplicate {
			return Migration{}, fmt.Errorf("%w: duplicate retry ID %q", ErrInvalidThreadWatchState, id)
		}
		seenRetries[id] = struct{}{}
		record, ok := migrated.Tasks[id]
		if !ok {
			return Migration{}, fmt.Errorf("%w: retry ID %q has no thread", ErrInvalidThreadWatchState, id)
		}
		record.Retry = &state.Retry{
			Operation: "legacy_classification", ErrorCode: "threadwatch_retry", Attempts: 1,
			LastAttemptAt: completedAt, NextAttemptAt: completedAt,
		}
		migrated.Tasks[id] = record
	}
	if err := migrated.Validate(); err != nil {
		return Migration{}, fmt.Errorf("%w: %v", ErrInvalidThreadWatchState, err)
	}
	return Migration{ControlTaskID: *legacy.ControllerThreadID, State: migrated}, nil
}

func statusFromLegacyTitle(title string) state.TaskStatus {
	title = strings.TrimSpace(title)
	for prefix, status := range map[string]state.TaskStatus{
		"⏳":  state.StatusRunning,
		"🚨":  state.StatusBlocked,
		"🙋":  state.StatusNeedsInput,
		"🤖":  state.StatusAutomation,
		"➡️": state.StatusNextSteps,
		"✅":  state.StatusComplete,
		"❔":  state.StatusUnknown,
	} {
		if strings.HasPrefix(title, prefix) {
			return status
		}
	}
	return state.StatusUnknown
}

func canonical(value string) bool {
	return value != "" && strings.TrimSpace(value) == value
}
