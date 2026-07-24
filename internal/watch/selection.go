package watch

import (
	"strings"
	"time"

	"github.com/ericlitman/threadbear/internal/codex"
	"github.com/ericlitman/threadbear/internal/codex/appserver"
	"github.com/ericlitman/threadbear/internal/state"
	"github.com/ericlitman/threadbear/internal/status"
)

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
	facts.RuntimeActive = evidence.ThreadStatus.Type == "active"
	if latest != nil {
		facts.RuntimeActive = facts.RuntimeActive || latest.Status == "inProgress" || latest.Status == "in_progress"
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
