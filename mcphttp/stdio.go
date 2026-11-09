package mcphttp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

// StdioClient connects to an MCP server running as a local child process.
type StdioClient struct {
	server    string
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	writeMu   sync.Mutex
	nextID    atomic.Int64
	pendingMu sync.Mutex
	pending   map[int64]chan *rpcResponse
	closed    atomic.Bool
	stopCh    chan struct{}
}

// NewStdio spawns an MCP server subprocess and performs the initialize handshake.
func NewStdio(ctx context.Context, server, command string, env map[string]string, args ...string) (*StdioClient, error) {
	cmd := exec.Command(command, args...)
	if len(env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}

	c := &StdioClient{
		server:  server,
		cmd:     cmd,
		stdin:   stdin,
		pending: make(map[int64]chan *rpcResponse),
		stopCh:  make(chan struct{}),
	}

	go c.readLoop(stdout)

	if err := c.initialize(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

func (c *StdioClient) readLoop(r io.Reader) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 16*1024*1024) // up to 16MB lines for large tools
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}
		c.pendingMu.Lock()
		ch, ok := c.pending[int64(resp.ID)]
		if ok {
			delete(c.pending, int64(resp.ID))
		}
		c.pendingMu.Unlock()
		if ok && ch != nil {
			ch <- &resp
		}
	}
	c.pendingMu.Lock()
	for _, ch := range c.pending {
		close(ch)
	}
	c.pending = make(map[int64]chan *rpcResponse)
	c.pendingMu.Unlock()
}

func (c *StdioClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	ch := make(chan *rpcResponse, 1)

	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      int(id),
		Method:  method,
		Params:  params,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')

	c.writeMu.Lock()
	_, err = c.stdin.Write(data)
	c.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("mcp stdio write: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.stopCh:
		return nil, errors.New("mcp stdio client closed")
	case resp, ok := <-ch:
		if !ok || resp == nil {
			return nil, errors.New("mcp stdio stream closed")
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

func (c *StdioClient) notify(method string, params any) error {
	req := rpcRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.stdin.Write(data)
	return err
}

func (c *StdioClient) initialize(ctx context.Context) error {
	if _, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": legacySSEProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "artex", "version": "0.2"},
	}); err != nil {
		return err
	}
	return c.notify("notifications/initialized", map[string]any{})
}

// Tools lists the server's tools and adapts them to actool.CoreTool.
func (c *StdioClient) Tools(ctx context.Context) ([]actool.CoreTool, error) {
	raw, err := c.call(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var res struct {
		Tools []remoteTool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	out := make([]actool.CoreTool, 0, len(res.Tools))
	for _, rt := range res.Tools {
		out = append(out, c.wrap(rt))
	}
	return out, nil
}

func (c *StdioClient) wrap(rt remoteTool) actool.CoreTool {
	schema := rt.InputSchema
	if schema == nil {
		schema = map[string]any{"type": "object"}
	}
	full := ToolName(c.server, rt.Name)
	return actool.Build(actool.Spec{
		Name:        full,
		Description: rt.Description,
		Schema:      schema,
		Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
			return permission.AskUser("call MCP tool " + full + "?")
		},
		Run: func(ctx context.Context, in json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
			var args any
			if len(in) > 0 {
				_ = json.Unmarshal(in, &args)
			}
			raw, err := c.call(ctx, "tools/call", map[string]any{"name": rt.Name, "arguments": args})
			if err != nil {
				return actool.Errorf("Error: " + err.Error()), nil
			}
			var res struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
				IsError bool `json:"isError"`
			}
			if err := json.Unmarshal(raw, &res); err != nil {
				return actool.Errorf("Error: bad MCP response: " + err.Error()), nil
			}
			var text string
			for _, blk := range res.Content {
				text += blk.Text
			}
			r := actool.Text(actool.Capture(tc, text))
			r.IsError = res.IsError
			return r, nil
		},
	})
}

// Close terminates the child process and cleans up pipes.
func (c *StdioClient) Close() error {
	if c.closed.Swap(true) {
		return nil
	}
	close(c.stopCh)
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
	return nil
}
