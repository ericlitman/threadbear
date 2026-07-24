package appserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type requestMessage struct {
	ID     int64  `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}
type notificationMessage struct {
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}
type responseMessage struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}
type wireMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("app server error %d: %s", e.Code, e.Message)
}

type Notification struct {
	Method string
	Params json.RawMessage
}
type ThreadStatus struct {
	Type        string   `json:"type"`
	ActiveFlags []string `json:"activeFlags,omitempty"`
}
type Thread struct {
	ID        string       `json:"id"`
	Name      string       `json:"name,omitempty"`
	Ephemeral bool         `json:"ephemeral,omitempty"`
	Path      *string      `json:"path,omitempty"`
	Status    ThreadStatus `json:"status"`
	Turns     []Turn       `json:"turns,omitempty"`
}
type Turn struct {
	ID     string          `json:"id"`
	Status string          `json:"status"`
	Error  *TurnError      `json:"error,omitempty"`
	Items  []TurnItem      `json:"items,omitempty"`
	Raw    json.RawMessage `json:"-"`
}

func (t *Turn) UnmarshalJSON(data []byte) error {
	type alias Turn
	var value alias
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*t = Turn(value)
	t.Raw = append(t.Raw[:0], data...)
	return nil
}

type TurnError struct {
	Message        string          `json:"message,omitempty"`
	CodexErrorInfo json.RawMessage `json:"codexErrorInfo,omitempty"`
}
type TurnItem struct {
	Type    string          `json:"type"`
	Phase   string          `json:"phase,omitempty"`
	Text    string          `json:"text,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
}
type EvidenceTurn struct {
	ID           string
	Status       string
	Error        *TurnError
	UserMessage  string
	AgentMessage string
}
type RecentEvidence struct {
	ThreadStatus ThreadStatus
	Latest       *EvidenceTurn
	Previous     *EvidenceTurn
}

func evidenceFromTurns(status ThreadStatus, turns []Turn) RecentEvidence {
	result := RecentEvidence{ThreadStatus: status}
	if len(turns) == 0 {
		return result
	}
	latest := evidenceFromTurn(turns[len(turns)-1])
	result.Latest = &latest
	if len(turns) > 1 {
		previous := evidenceFromTurn(turns[len(turns)-2])
		result.Previous = &previous
	}
	return result
}
func evidenceFromDescendingTurns(status ThreadStatus, turns []Turn) RecentEvidence {
	result := RecentEvidence{ThreadStatus: status}
	if len(turns) == 0 {
		return result
	}
	latest := evidenceFromTurn(turns[0])
	result.Latest = &latest
	if len(turns) > 1 {
		previous := evidenceFromTurn(turns[1])
		result.Previous = &previous
	}
	return result
}
func evidenceFromTurn(turn Turn) EvidenceTurn {
	result := EvidenceTurn{ID: turn.ID, Status: turn.Status, Error: turn.Error}
	for _, item := range turn.Items {
		switch item.Type {
		case "userMessage", "user_message":
			if text := item.messageText(); text != "" {
				result.UserMessage = text
			}
		case "agentMessage", "agent_message":
			if item.Phase != "" && item.Phase != "final_answer" && item.Phase != "finalAnswer" {
				continue
			}
			if text := item.messageText(); text != "" {
				result.AgentMessage = text
			}
		}
	}
	return result
}
func (i TurnItem) messageText() string {
	if i.Text != "" {
		return i.Text
	}
	if len(i.Content) == 0 || bytes.Equal(i.Content, []byte("null")) {
		return ""
	}
	var direct string
	if json.Unmarshal(i.Content, &direct) == nil {
		return direct
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(i.Content, &parts); err != nil {
		return ""
	}
	var text strings.Builder
	for _, part := range parts {
		switch part.Type {
		case "inputText", "input_text", "outputText", "output_text", "text":
			text.WriteString(part.Text)
		}
	}
	return text.String()
}

type ToolRestriction struct {
	ConfigOverride       bool     `json:"configOverride"`
	PermissionProfile    bool     `json:"permissionProfile"`
	EnvironmentsDisabled bool     `json:"environmentsDisabled"`
	DynamicToolsDisabled bool     `json:"dynamicToolsDisabled"`
	ApprovalsDisabled    bool     `json:"approvalsDisabled"`
	ReadOnlySandbox      bool     `json:"readOnlySandbox"`
	OutputConstrained    bool     `json:"outputConstrained"`
	UnprovenToolSources  []string `json:"unprovenToolSources"`
}

type EphemeralRequest struct {
	Model             string
	Effort            string
	Input             string
	OutputSchema      any
	ToolConfig        map[string]any
	PermissionProfile string
}
type EphemeralResult struct {
	ThreadID        string
	Turn            Turn
	ToolRestriction ToolRestriction
}

func validateEphemeralRequest(request EphemeralRequest) error {
	if strings.TrimSpace(request.Model) == "" {
		return errors.New("classifier model is required")
	}
	if strings.TrimSpace(request.Effort) == "" {
		return errors.New("classifier effort is required")
	}
	if request.OutputSchema == nil {
		return errors.New("classifier output schema is required")
	}
	return nil
}
