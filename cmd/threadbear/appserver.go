package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync/atomic"
	"syscall"
)

type appClient struct {
	cmd     *exec.Cmd
	in      io.WriteCloser
	scanner *bufio.Scanner
	next    atomic.Int64
}

type appThread struct {
	ID     string
	Status struct {
		Type        string   `json:"type"`
		ActiveFlags []string `json:"activeFlags"`
	} `json:"status"`
}

func openApp(ctx context.Context, codexPath string) (*appClient, error) {
	if codexPath == "" {
		codexPath = "codex"
	}
	cmd := exec.CommandContext(ctx, codexPath, "app-server", "--listen", "stdio://")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		_ = in.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = in.Close()
		_ = out.Close()
		return nil, err
	}
	client := &appClient{cmd: cmd, in: in, scanner: bufio.NewScanner(out)}
	client.scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var initialized any
	if err := client.call("initialize", map[string]any{
		"clientInfo":   map[string]string{"name": "threadbear", "title": "ThreadBear", "version": version},
		"capabilities": map[string]bool{"experimentalApi": true},
	}, &initialized); err != nil {
		client.close()
		return nil, err
	}
	if err := client.write(map[string]any{"method": "initialized"}); err != nil {
		client.close()
		return nil, err
	}
	return client, nil
}

func (c *appClient) write(value any) error {
	return json.NewEncoder(c.in).Encode(value)
}

func (c *appClient) call(method string, params any, result any) error {
	id := c.next.Add(1)
	if err := c.write(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	for c.scanner.Scan() {
		var message struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(c.scanner.Bytes(), &message) != nil || message.ID != id {
			continue
		}
		if message.Error != nil {
			return fmt.Errorf("App Server %s failed (%d): %s", method, message.Error.Code, message.Error.Message)
		}
		if result != nil && len(message.Result) != 0 {
			return json.Unmarshal(message.Result, result)
		}
		return nil
	}
	return errors.Join(errors.New("App Server closed"), c.scanner.Err())
}

func (c *appClient) readThread(id string) (appThread, error) {
	var response struct {
		Thread appThread `json:"thread"`
	}
	err := c.call("thread/read", map[string]any{"threadId": id, "includeTurns": false}, &response)
	if err == nil && response.Thread.ID != id {
		err = fmt.Errorf("App Server returned task %q for %q", response.Thread.ID, id)
	}
	return response.Thread, err
}

func (c *appClient) close() error {
	_ = c.in.Close()
	if c.cmd.Process != nil {
		_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
		_ = c.cmd.Process.Kill()
	}
	return c.cmd.Wait()
}
