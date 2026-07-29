package watch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/ericlitman/threadbear/internal/codex"
	"github.com/ericlitman/threadbear/internal/codex/appserver"
	"github.com/ericlitman/threadbear/internal/state"
	"github.com/ericlitman/threadbear/internal/status"
)

func evidenceFingerprint(task codex.Task, evidence appserver.RecentEvidence) string {
	flags := append([]string(nil), evidence.ThreadStatus.ActiveFlags...)
	sort.Strings(flags)
	payload := struct {
		ThreadType   string   `json:"thread_type"`
		ThreadSource string   `json:"thread_source"`
		Flags        []string `json:"flags"`
		LatestID     string   `json:"latest_id"`
		LatestStatus string   `json:"latest_status"`
		LatestError  bool     `json:"latest_error"`
		ErrorMessage string   `json:"error_message"`
		ErrorInfo    string   `json:"error_info"`
		UserMessage  string   `json:"user_message"`
		AgentMessage string   `json:"agent_message"`
	}{ThreadType: evidence.ThreadStatus.Type, ThreadSource: task.ThreadSource, Flags: flags}
	if evidence.Latest != nil {
		payload.LatestID = evidence.Latest.ID
		payload.LatestStatus = evidence.Latest.Status
		payload.UserMessage = evidence.Latest.UserMessage
		payload.AgentMessage = evidence.Latest.AgentMessage
		if evidence.Latest.Error != nil {
			payload.LatestError = true
			payload.ErrorMessage = evidence.Latest.Error.Message
			payload.ErrorInfo = string(evidence.Latest.Error.CodexErrorInfo)
		}
	}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type selectedTask struct {
	Task       codex.Task
	Evidence   appserver.RecentEvidence
	Resolution status.Resolution
	Activity   time.Time
}

func selectTask(task codex.Task, evidence appserver.RecentEvidence, capturedAt time.Time) selectedTask {
	latest := evidence.Latest
	facts := status.Facts{}
	for _, flag := range evidence.ThreadStatus.ActiveFlags {
		switch flag {
		case "waitingOnApproval", "waiting_on_approval", "waitingOnUserInput", "waiting_on_user_input":
			facts.WaitingForUser = true
		}
	}
	facts.RuntimeActive = evidence.Active()
	if latest != nil {
		facts.StructuredFailure = latest.Error != nil || latest.Status == "failed"
		facts.Interrupted = latest.Status == "interrupted" || latest.Status == "cancelled" || latest.Status == "canceled"
		facts.Footer = status.FooterInput{
			Message:             latest.AgentMessage,
			LatestTurnCompleted: latest.Status == "completed",
			StructuredStatus:    structuredStatus(facts),
		}
	}
	facts.HealthyIdleAutomation = registeredAutomation(task.ThreadSource) && evidence.ThreadStatus.Type == "idle"
	activity := capturedAt.UTC()
	if evidence.RecencyAt != nil && *evidence.RecencyAt > 0 {
		activity = time.Unix(*evidence.RecencyAt, 0).UTC()
	}
	return selectedTask{Task: task, Evidence: evidence, Resolution: status.Resolve(facts), Activity: activity}
}

func structuredStatus(facts status.Facts) state.TaskStatus {
	switch {
	case facts.WaitingForUser:
		return state.StatusNeedsInput
	case facts.RuntimeActive:
		return state.StatusRunning
	case facts.StructuredFailure:
		return state.StatusBlocked
	case facts.HealthyIdleAutomation:
		return state.StatusAutomation
	case facts.Interrupted:
		return state.StatusUnknown
	default:
		return ""
	}
}

func registeredAutomation(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "automation", "scheduled", "scheduled_task", "scheduledtask":
		return true
	default:
		return false
	}
}

func taskEvidence(task selectedTask) status.TaskEvidence {
	result := status.TaskEvidence{TaskID: task.Task.TaskID, Revision: task.Task.Revision}
	if task.Evidence.Latest != nil {
		result.Latest = status.TurnEvidence{User: task.Evidence.Latest.UserMessage, FinalAgent: task.Resolution.ClassifierMessage}
	}
	return result
}
