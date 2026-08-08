package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const (
	appServerCurrentTimeout    = 3 * time.Second
	appServerListLimit         = 100
	appServerListTimeout       = 30 * time.Second
	appServerOnboardingTimeout = 10 * time.Minute
)

var (
	appServerCurrentBudget    = appServerCurrentTimeout
	appServerListBudget       = appServerListTimeout
	appServerOnboardingBudget = appServerOnboardingTimeout
)

type appServerRPCMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type appServerRPCError struct {
	Code int `json:"code"`
}

type appServerThread struct {
	ID   *string `json:"id"`
	Name *string `json:"name"`
}

type appServerThreadPage struct {
	Data       json.RawMessage `json:"data"`
	NextCursor json.RawMessage `json:"nextCursor"`
}

type appServerClient struct {
	ctx     context.Context
	cancel  context.CancelFunc
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	encoder *json.Encoder
	decoder *json.Decoder
	waited  bool
}

func startAppServer(ctx context.Context, timeout time.Duration) (_ *appServerClient, err error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	cmd := exec.CommandContext(runCtx, "codex", "app-server", "--stdio")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open Codex App Server input: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open Codex App Server output: %w", err)
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start Codex App Server: %w", err)
	}
	client := &appServerClient{
		ctx: runCtx, cancel: cancel, cmd: cmd, stdin: stdin,
		encoder: json.NewEncoder(stdin), decoder: json.NewDecoder(stdout),
	}
	defer func() {
		if err != nil {
			client.abort()
		}
	}()
	if err := client.encoder.Encode(map[string]any{
		"id": 1, "method": "initialize",
		"params": map[string]any{"clientInfo": map[string]string{"name": "threadbear", "version": version}},
	}); err != nil {
		return nil, client.ioError("initialize Codex App Server", err)
	}
	initialized, err := readAppServerResponse(client.decoder, 1)
	if err != nil {
		return nil, client.ioError("initialize Codex App Server", err)
	}
	var initializeResult map[string]json.RawMessage
	if json.Unmarshal(initialized, &initializeResult) != nil || initializeResult == nil {
		return nil, errors.New("initialize Codex App Server: invalid result")
	}
	if err := client.encoder.Encode(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return nil, client.ioError("notify Codex App Server initialization", err)
	}
	return client, nil
}

func (client *appServerClient) request(id int, method string, params map[string]any, operation string) (json.RawMessage, error) {
	if err := client.encoder.Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, client.ioError(operation, err)
	}
	result, err := readAppServerResponse(client.decoder, id)
	if err != nil {
		return nil, client.ioError(operation, err)
	}
	return result, nil
}

func (client *appServerClient) close() {
	_ = client.stdin.Close()
	client.cancel()
	_ = client.cmd.Wait()
	client.waited = true
}

func (client *appServerClient) abort() {
	if client.waited {
		return
	}
	client.cancel()
	_ = client.stdin.Close()
	_ = client.cmd.Wait()
	client.waited = true
}

func (client *appServerClient) ioError(operation string, err error) error {
	if client.ctx.Err() != nil {
		return fmt.Errorf("%s: %w", operation, client.ctx.Err())
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func (client *appServerClient) currentTask(requestID int, id string) (indexedTask, error) {
	seenCursors := make(map[string]struct{})
	var cursor *string
	for pageNumber := 1; ; pageNumber++ {
		params := map[string]any{
			"archived": false, "limit": appServerListLimit,
			"sortKey": "recency_at", "sortDirection": "desc",
		}
		if cursor != nil {
			params["cursor"] = *cursor
		}
		operation := fmt.Sprintf("read Codex App Server current thread/list page %d", pageNumber)
		result, err := client.request(requestID, "thread/list", params, operation)
		if err != nil {
			return indexedTask{}, err
		}
		requestID++
		page, next, err := decodeAppServerThreadPage(result)
		if err != nil {
			return indexedTask{}, fmt.Errorf("%s: %w", operation, err)
		}
		for index := range page {
			if page[index].ID != nil && *page[index].ID == id {
				return indexedTaskFromAppServer(page[index])
			}
		}
		if next == nil {
			break
		}
		if *next == "" {
			return indexedTask{}, fmt.Errorf("%s: empty next cursor", operation)
		}
		if _, repeated := seenCursors[*next]; repeated {
			return indexedTask{}, fmt.Errorf("%s: repeated next cursor", operation)
		}
		seenCursors[*next] = struct{}{}
		cursor = next
	}
	return indexedTask{}, errors.New("read Codex App Server current thread/list: current task is absent")
}

func (client *appServerClient) inventory(nextRequestID *int) ([]indexedTask, error) {
	all := make([]appServerThread, 0)
	seenCursors := make(map[string]struct{})
	var cursor *string
	for pageNumber := 1; ; pageNumber++ {
		params := map[string]any{"archived": false, "limit": appServerListLimit}
		if cursor != nil {
			params["cursor"] = *cursor
		}
		requestID := *nextRequestID
		*nextRequestID = requestID + 1
		result, err := client.request(requestID, "thread/list", params,
			fmt.Sprintf("read Codex App Server thread/list page %d", pageNumber))
		if err != nil {
			return nil, err
		}
		page, next, err := decodeAppServerThreadPage(result)
		if err != nil {
			return nil, fmt.Errorf("read Codex App Server thread/list page %d: %w", pageNumber, err)
		}
		all = append(all, page...)
		if next == nil {
			break
		}
		if *next == "" {
			return nil, fmt.Errorf("read Codex App Server thread/list page %d: empty next cursor", pageNumber)
		}
		if _, repeated := seenCursors[*next]; repeated {
			return nil, fmt.Errorf("read Codex App Server thread/list page %d: repeated next cursor", pageNumber)
		}
		seenCursors[*next] = struct{}{}
		cursor = next
	}
	return finishAppServerInventory(all)
}

func indexedTaskFromAppServer(thread appServerThread) (indexedTask, error) {
	if thread.ID == nil || *thread.ID == "" {
		return indexedTask{}, errors.New("Codex App Server returned an invalid task")
	}
	task := indexedTask{ID: *thread.ID}
	if thread.Name == nil || strings.TrimSpace(*thread.Name) == "" {
		task.RawFallback = true
	} else {
		task.Title = *thread.Name
	}
	return task, nil
}

func readAppServerResponse(decoder *json.Decoder, wantID int) (json.RawMessage, error) {
	for {
		var message appServerRPCMessage
		if err := decoder.Decode(&message); err != nil {
			return nil, err
		}
		if len(message.ID) == 0 {
			if message.Method == "" {
				return nil, errors.New("invalid Codex App Server message")
			}
			continue
		}
		var gotID int
		if json.Unmarshal(message.ID, &gotID) != nil || gotID != wantID {
			return nil, errors.New("unexpected Codex App Server response ID")
		}
		if len(message.Error) != 0 && string(message.Error) != "null" {
			var responseError appServerRPCError
			if json.Unmarshal(message.Error, &responseError) != nil {
				return nil, errors.New("invalid Codex App Server error response")
			}
			return nil, fmt.Errorf("Codex App Server request failed with code %d", responseError.Code)
		}
		if len(message.Result) == 0 || string(message.Result) == "null" {
			return nil, errors.New("Codex App Server response has no result")
		}
		return message.Result, nil
	}
}

func decodeAppServerThreadPage(result json.RawMessage) ([]appServerThread, *string, error) {
	var page appServerThreadPage
	if json.Unmarshal(result, &page) != nil || len(page.Data) == 0 || !strings.HasPrefix(strings.TrimSpace(string(page.Data)), "[") {
		return nil, nil, errors.New("invalid thread/list result")
	}
	var threads []appServerThread
	if err := json.Unmarshal(page.Data, &threads); err != nil {
		return nil, nil, errors.New("invalid thread/list data")
	}
	if len(page.NextCursor) == 0 || string(page.NextCursor) == "null" {
		return threads, nil, nil
	}
	var next string
	if json.Unmarshal(page.NextCursor, &next) != nil {
		return nil, nil, errors.New("invalid thread/list next cursor")
	}
	return threads, &next, nil
}

func finishAppServerInventory(all []appServerThread) ([]indexedTask, error) {
	tasks := make([]indexedTask, 0, len(all))
	seen := make(map[string]struct{}, len(all))
	for _, thread := range all {
		task, err := indexedTaskFromAppServer(thread)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[task.ID]; duplicate {
			continue
		}
		seen[task.ID] = struct{}{}
		tasks = append(tasks, task)
	}
	sort.SliceStable(tasks, func(left, right int) bool { return tasks[left].ID < tasks[right].ID })
	return tasks, nil
}
