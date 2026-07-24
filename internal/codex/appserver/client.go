package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrClosed            = errors.New("App Server client is closed")
	ErrUnexpectedRequest = errors.New("unexpected App Server request")
)

type Client struct {
	capabilities      Capabilities
	command           *exec.Cmd
	stdin             io.WriteCloser
	nextID            atomic.Int64
	write             sync.Mutex
	mu                sync.Mutex
	pending           map[int64]chan responseMessage
	err               error
	notificationInput chan Notification
	notifications     chan Notification
	done              chan struct{}
	wait              chan error
	closeOnce         sync.Once
}

func Start(ctx context.Context, process ProcessSpec, capabilities Capabilities) (*Client, error) {
	command, err := process.command(ctx, "app-server", "--listen", "stdio://")
	if err != nil {
		return nil, err
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open App Server stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("open App Server stdout: %w", err)
	}
	if err := command.Start(); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("start App Server: %w", err)
	}
	client := &Client{capabilities: capabilities, command: command, stdin: stdin, pending: make(map[int64]chan responseMessage), notificationInput: make(chan Notification), notifications: make(chan Notification), done: make(chan struct{}), wait: make(chan error, 1)}
	go client.notificationLoop()
	go client.readLoop(stdout)
	go func() { err := command.Wait(); client.wait <- err; client.fail(processExitError(err)) }()
	initialize := map[string]any{"clientInfo": map[string]any{"name": "threadbear", "title": "ThreadBear", "version": "0.0.0"}, "capabilities": map[string]any{"experimentalApi": true}}
	var initialized struct {
		CodexHome string `json:"codexHome"`
	}
	if err := client.call(ctx, "initialize", initialize, &initialized); err != nil {
		client.Close()
		return nil, fmt.Errorf("initialize App Server: %w", err)
	}
	if err := client.notify("initialized", nil); err != nil {
		client.Close()
		return nil, fmt.Errorf("acknowledge App Server initialization: %w", err)
	}
	return client, nil
}
func processExitError(err error) error {
	if err == nil {
		return errors.New("App Server exited")
	}
	return fmt.Errorf("App Server exited: %w", err)
}
func (c *Client) Capabilities() Capabilities         { return c.capabilities }
func (c *Client) Notifications() <-chan Notification { return c.notifications }
func (c *Client) notificationLoop() {
	queue := make([]Notification, 0)
	for {
		var output chan Notification
		var next Notification
		if len(queue) > 0 {
			output = c.notifications
			next = queue[0]
		}
		select {
		case <-c.done:
			return
		case notification := <-c.notificationInput:
			queue = append(queue, notification)
		case output <- next:
			queue[0] = Notification{}
			queue = queue[1:]
		}
	}
}
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.stdin.Close()
		select {
		case <-c.wait:
		case <-time.After(2 * time.Second):
			if c.command.Process != nil {
				c.command.Process.Kill()
			}
			<-c.wait
		}
		c.fail(ErrClosed)
	})
	c.mu.Lock()
	defer c.mu.Unlock()
	if errors.Is(c.err, ErrClosed) || strings.HasPrefix(errorString(c.err), "App Server exited") {
		return nil
	}
	return c.err
}
func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
func (c *Client) readLoop(reader io.Reader) {
	buffer := bufio.NewReader(reader)
	for {
		line, err := buffer.ReadBytes(byte(10))
		if len(strings.TrimSpace(string(line))) > 0 {
			if decodeErr := c.handleLine(line); decodeErr != nil {
				c.fail(decodeErr)
				if c.command.Process != nil {
					c.command.Process.Kill()
				}
				return
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				c.fail(fmt.Errorf("read App Server response: %w", err))
			}
			return
		}
	}
}
func (c *Client) handleLine(line []byte) error {
	var message wireMessage
	if err := json.Unmarshal(line, &message); err != nil {
		return fmt.Errorf("decode App Server message: %w", err)
	}
	if message.Method != "" && len(message.ID) > 0 && string(message.ID) != "null" {
		return fmt.Errorf("%w: %s", ErrUnexpectedRequest, message.Method)
	}
	if message.Method != "" {
		notification := Notification{Method: message.Method, Params: append(json.RawMessage(nil), message.Params...)}
		select {
		case c.notificationInput <- notification:
			return nil
		case <-c.done:
			return ErrClosed
		}
	}
	if len(message.ID) == 0 {
		return errors.New("App Server message has neither method nor ID")
	}
	id, err := parseResponseID(message.ID)
	if err != nil {
		return err
	}
	c.mu.Lock()
	response := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if response != nil {
		response <- responseMessage{ID: id, Result: message.Result, Error: message.Error}
	}
	return nil
}
func parseResponseID(raw json.RawMessage) (int64, error) {
	var id int64
	if err := json.Unmarshal(raw, &id); err == nil {
		return id, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if parsed, parseErr := strconv.ParseInt(text, 10, 64); parseErr == nil {
			return parsed, nil
		}
	}
	return 0, fmt.Errorf("invalid App Server response ID: %s", raw)
}
func (c *Client) call(ctx context.Context, method string, params any, result any) error {
	id := c.nextID.Add(1)
	response := make(chan responseMessage, 1)
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return err
	}
	c.pending[id] = response
	c.mu.Unlock()
	if err := c.send(requestMessage{ID: id, Method: method, Params: params}); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return err
	}
	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return fmt.Errorf("App Server %s: %w", method, ctx.Err())
	case <-c.done:
		c.mu.Lock()
		err := c.err
		c.mu.Unlock()
		if err == nil {
			err = ErrClosed
		}
		return err
	case message := <-response:
		if message.Error != nil {
			return message.Error
		}
		if result == nil || len(message.Result) == 0 || string(message.Result) == "null" {
			return nil
		}
		if err := json.Unmarshal(message.Result, result); err != nil {
			return fmt.Errorf("decode App Server %s result: %w", method, err)
		}
		return nil
	}
}
func (c *Client) notify(method string, params any) error {
	return c.send(notificationMessage{Method: method, Params: params})
}
func (c *Client) send(message any) error {
	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode App Server message: %w", err)
	}
	data = append(data, byte(10))
	c.write.Lock()
	defer c.write.Unlock()
	if _, err := c.stdin.Write(data); err != nil {
		return fmt.Errorf("write App Server message: %w", err)
	}
	return nil
}
func (c *Client) fail(err error) {
	if err == nil {
		return
	}
	c.mu.Lock()
	if c.err == nil {
		c.err = err
		close(c.done)
	}
	c.mu.Unlock()
}
func (c *Client) ReadLatestTurn(ctx context.Context, threadID, rolloutPath string) (RecentEvidence, error) {
	if strings.TrimSpace(threadID) == "" {
		return RecentEvidence{}, errors.New("thread ID is required")
	}
	switch c.capabilities.RecentTurnsMethod() {
	case "thread/turns/list":
		var page struct {
			Data []Turn `json:"data"`
		}
		if err := c.call(ctx, "thread/turns/list", map[string]any{"threadId": threadID, "limit": 1, "sortDirection": "desc", "itemsView": "full"}, &page); err != nil {
			return RecentEvidence{}, err
		}
		var status ThreadStatus
		var recencyAt *int64
		if c.capabilities.HasMethod("thread/read") {
			var read struct {
				Thread Thread `json:"thread"`
			}
			if err := c.call(ctx, "thread/read", map[string]any{"threadId": threadID, "includeTurns": false}, &read); err != nil {
				return RecentEvidence{}, err
			}
			status = read.Thread.Status
			recencyAt = read.Thread.RecencyAt
		}
		result := evidenceFromDescendingTurns(status, recencyAt, page.Data)
		result.Previous = nil
		return result, nil
	case "thread/read":
		var read struct {
			Thread Thread `json:"thread"`
		}
		if err := c.call(ctx, "thread/read", map[string]any{"threadId": threadID, "includeTurns": true}, &read); err != nil {
			return RecentEvidence{}, err
		}
		result := evidenceFromTurns(read.Thread.Status, read.Thread.RecencyAt, read.Thread.Turns)
		result.Previous = nil
		return result, nil
	default:
		if rolloutPath == "" {
			return RecentEvidence{}, fmt.Errorf("%w: latest-turn read", ErrCapability)
		}
		result, err := readRolloutEvidence(rolloutPath, 1)
		result.Previous = nil
		return result, err
	}
}

func (c *Client) ReadPreviousTurn(ctx context.Context, threadID, rolloutPath string) (*EvidenceTurn, error) {
	evidence, err := c.ReadRecentTurns(ctx, threadID, rolloutPath)
	if err != nil {
		return nil, err
	}
	return evidence.Previous, nil
}

func (c *Client) ReadRecentTurns(ctx context.Context, threadID, rolloutPath string) (RecentEvidence, error) {
	if strings.TrimSpace(threadID) == "" {
		return RecentEvidence{}, errors.New("thread ID is required")
	}
	switch c.capabilities.RecentTurnsMethod() {
	case "thread/turns/list":
		var page struct {
			Data []Turn `json:"data"`
		}
		if err := c.call(ctx, "thread/turns/list", map[string]any{"threadId": threadID, "limit": 2, "sortDirection": "desc", "itemsView": "full"}, &page); err != nil {
			return RecentEvidence{}, err
		}
		var status ThreadStatus
		var recencyAt *int64
		if c.capabilities.HasMethod("thread/read") {
			var read struct {
				Thread Thread `json:"thread"`
			}
			if err := c.call(ctx, "thread/read", map[string]any{"threadId": threadID, "includeTurns": false}, &read); err != nil {
				return RecentEvidence{}, err
			}
			status = read.Thread.Status
			recencyAt = read.Thread.RecencyAt
		}
		return evidenceFromDescendingTurns(status, recencyAt, page.Data), nil
	case "thread/read":
		var read struct {
			Thread Thread `json:"thread"`
		}
		if err := c.call(ctx, "thread/read", map[string]any{"threadId": threadID, "includeTurns": true}, &read); err != nil {
			return RecentEvidence{}, err
		}
		return evidenceFromTurns(read.Thread.Status, read.Thread.RecencyAt, read.Thread.Turns), nil
	default:
		if rolloutPath == "" {
			return RecentEvidence{}, fmt.Errorf("%w: recent-turn read", ErrCapability)
		}
		return readRolloutEvidence(rolloutPath, 2)
	}
}
func (c *Client) SetTitle(ctx context.Context, threadID, title string) error {
	if err := c.capabilities.requireMethod("thread/name/set"); err != nil {
		return err
	}
	return c.call(ctx, "thread/name/set", map[string]any{"threadId": threadID, "name": title}, nil)
}
func (c *Client) Archive(ctx context.Context, threadID string) error {
	if err := c.capabilities.requireMethod("thread/archive"); err != nil {
		return err
	}
	return c.call(ctx, "thread/archive", map[string]any{"threadId": threadID}, nil)
}
func (c *Client) Unarchive(ctx context.Context, threadID string) (Thread, error) {
	if err := c.capabilities.requireMethod("thread/unarchive"); err != nil {
		return Thread{}, err
	}
	var response struct {
		Thread Thread `json:"thread"`
	}
	if err := c.call(ctx, "thread/unarchive", map[string]any{"threadId": threadID}, &response); err != nil {
		return Thread{}, err
	}
	return response.Thread, nil
}
func (c *Client) CountNotice(ctx context.Context, threadID, text string) (int, error) {
	if err := c.capabilities.requireMethod("thread/read"); err != nil {
		return 0, err
	}
	if strings.TrimSpace(threadID) == "" || strings.TrimSpace(text) == "" {
		return 0, errors.New("thread ID and notice text are required")
	}
	var read struct {
		Thread Thread `json:"thread"`
	}
	if err := c.call(ctx, "thread/read", map[string]any{"threadId": threadID, "includeTurns": false}, &read); err != nil {
		return 0, err
	}
	if read.Thread.Path == nil || strings.TrimSpace(*read.Thread.Path) == "" {
		return 0, errors.New("control thread rollout path is unavailable")
	}
	return countNoticeInRollout(*read.Thread.Path, text)
}

func countNoticeInRollout(path, text string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open control rollout: %w", err)
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	count := 0
	for {
		line, readErr := reader.ReadBytes(byte(10))
		if len(strings.TrimSpace(string(line))) > 0 {
			var envelope struct {
				Type    string          `json:"type"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal(line, &envelope); err != nil {
				return 0, fmt.Errorf("parse control rollout: %w", err)
			}
			if envelope.Type == "response_item" {
				var item struct {
					Type    string          `json:"type"`
					Role    string          `json:"role"`
					Content json.RawMessage `json:"content"`
				}
				if json.Unmarshal(envelope.Payload, &item) == nil && item.Type == "message" && item.Role == "assistant" && (TurnItem{Content: item.Content}).messageText() == text {
					count++
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return count, nil
			}
			return 0, fmt.Errorf("read control rollout: %w", readErr)
		}
	}
}

func (c *Client) InsertNotice(ctx context.Context, threadID, text string) error {
	if err := c.capabilities.requireMethod("thread/inject_items"); err != nil {
		return err
	}
	if strings.TrimSpace(text) == "" {
		return errors.New("notice text is required")
	}
	item := map[string]any{"type": "message", "role": "assistant", "content": []map[string]any{{"type": "output_text", "text": text}}}
	return c.call(ctx, "thread/inject_items", map[string]any{"threadId": threadID, "items": []any{item}}, nil)
}
func (c *Client) RunEphemeral(ctx context.Context, request EphemeralRequest) (EphemeralResult, error) {
	if err := validateEphemeralRequest(request); err != nil {
		return EphemeralResult{}, err
	}
	restriction, err := c.capabilities.requireEphemeral(request)
	if err != nil {
		return EphemeralResult{}, err
	}
	threadParams := map[string]any{"ephemeral": true, "model": request.Model, "environments": []any{}, "dynamicTools": []any{}, "approvalPolicy": "never"}
	if restriction.ConfigOverride {
		threadParams["config"] = request.ToolConfig
	}
	if restriction.PermissionProfile {
		threadParams["permissions"] = request.PermissionProfile
		restriction.ReadOnlySandbox = false
	} else {
		threadParams["sandbox"] = "read-only"
	}
	var started struct {
		Thread Thread `json:"thread"`
	}
	if err := c.call(ctx, "thread/start", threadParams, &started); err != nil {
		return EphemeralResult{}, err
	}
	if !started.Thread.Ephemeral || started.Thread.Path != nil {
		return EphemeralResult{}, errors.New("App Server did not confirm an in-memory ephemeral thread")
	}
	turnParams := map[string]any{"threadId": started.Thread.ID, "input": []map[string]any{{"type": "text", "text": request.Input}}, "model": request.Model, "effort": request.Effort, "outputSchema": request.OutputSchema, "environments": []any{}, "approvalPolicy": "never"}
	if restriction.PermissionProfile {
		turnParams["permissions"] = request.PermissionProfile
	} else {
		turnParams["sandboxPolicy"] = map[string]any{"type": "readOnly", "networkAccess": false}
	}
	var turnStarted struct {
		Turn Turn `json:"turn"`
	}
	if err := c.call(ctx, "turn/start", turnParams, &turnStarted); err != nil {
		return EphemeralResult{}, err
	}
	turn, err := c.waitForTurn(ctx, started.Thread.ID, turnStarted.Turn.ID)
	if err != nil {
		return EphemeralResult{}, err
	}
	return EphemeralResult{ThreadID: started.Thread.ID, Turn: turn, ToolRestriction: restriction}, nil
}
func (c *Client) waitForTurn(ctx context.Context, threadID, turnID string) (Turn, error) {
	items := make([]TurnItem, 0)
	for {
		select {
		case <-ctx.Done():
			return Turn{}, fmt.Errorf("wait for classifier turn: %w", ctx.Err())
		case <-c.done:
			c.mu.Lock()
			err := c.err
			c.mu.Unlock()
			return Turn{}, err
		case notification := <-c.notifications:
			switch notification.Method {
			case "item/completed":
				var completed struct {
					ThreadID string   `json:"threadId"`
					TurnID   string   `json:"turnId"`
					Item     TurnItem `json:"item"`
				}
				if err := json.Unmarshal(notification.Params, &completed); err != nil {
					return Turn{}, fmt.Errorf("decode item completion: %w", err)
				}
				if completed.ThreadID == threadID && completed.TurnID == turnID {
					items = append(items, completed.Item)
				}
			case "turn/completed":
				var completed struct {
					ThreadID string `json:"threadId"`
					Turn     Turn   `json:"turn"`
				}
				if err := json.Unmarshal(notification.Params, &completed); err != nil {
					return Turn{}, fmt.Errorf("decode turn completion: %w", err)
				}
				if completed.ThreadID == threadID && completed.Turn.ID == turnID {
					if len(completed.Turn.Items) == 0 {
						completed.Turn.Items = items
					}
					return completed.Turn, nil
				}
			}
		}
	}
}
func readRolloutEvidence(path string, limit int) (RecentEvidence, error) {
	file, err := os.Open(path)
	if err != nil {
		return RecentEvidence{}, fmt.Errorf("open Codex rollout: %w", err)
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	turns := make([]Turn, 0)
	turnCount := 0
	for {
		line, readErr := reader.ReadBytes(byte(10))
		if len(strings.TrimSpace(string(line))) > 0 {
			var envelope struct {
				Type    string          `json:"type"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal(line, &envelope); err != nil {
				return RecentEvidence{}, fmt.Errorf("parse Codex rollout: %w", err)
			}
			switch envelope.Type {
			case "response_item":
				var item struct {
					Type    string          `json:"type"`
					Role    string          `json:"role"`
					Content json.RawMessage `json:"content"`
				}
				if json.Unmarshal(envelope.Payload, &item) == nil && item.Type == "message" {
					mapped := TurnItem{Content: item.Content}
					switch item.Role {
					case "user":
						mapped.Type = "userMessage"
						turnCount++
						turns = append(turns, Turn{ID: fmt.Sprintf("rollout-%d", turnCount), Status: "inProgress", Items: []TurnItem{mapped}})
						if limit > 0 && len(turns) > limit {
							turns = append([]Turn{}, turns[len(turns)-limit:]...)
						}
					case "assistant":
						mapped.Type = "agentMessage"
						if len(turns) > 0 {
							turns[len(turns)-1].Items = append(turns[len(turns)-1].Items, mapped)
						}
					}
				}
			case "event_msg":
				if len(turns) == 0 {
					break
				}
				var event struct {
					Type    string          `json:"type"`
					Message string          `json:"message"`
					Reason  string          `json:"reason"`
					Error   *TurnError      `json:"error"`
					Info    json.RawMessage `json:"codex_error_info"`
				}
				if err := json.Unmarshal(envelope.Payload, &event); err != nil {
					return RecentEvidence{}, fmt.Errorf("parse Codex rollout event: %w", err)
				}
				turn := &turns[len(turns)-1]
				switch event.Type {
				case "turn_complete":
					if event.Error != nil {
						turn.Status = "failed"
						turn.Error = event.Error
					} else {
						turn.Status = "completed"
						markFinalAgentMessage(turn)
					}
				case "error":
					turn.Status = "failed"
					turn.Error = &TurnError{Message: event.Message, CodexErrorInfo: event.Info}
				case "turn_aborted":
					turn.Status = "interrupted"
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return RecentEvidence{}, fmt.Errorf("read Codex rollout: %w", readErr)
		}
	}
	if limit > 0 && len(turns) > limit {
		turns = turns[len(turns)-limit:]
	}
	return evidenceFromTurns(ThreadStatus{}, nil, turns), nil
}

func markFinalAgentMessage(turn *Turn) {
	for index := len(turn.Items) - 1; index >= 0; index-- {
		if turn.Items[index].Type == "agentMessage" {
			turn.Items[index].Phase = "final_answer"
			return
		}
	}
}
