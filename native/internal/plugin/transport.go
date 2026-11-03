package plugin

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

// maxFrame bounds one JSON-RPC frame. Oversized frames are a protocol error.
const maxFrame = 16 << 20

// callTimeout is the default per-call budget; callers may shorten it via ctx.
const callTimeout = 60 * time.Second

// RPCError is a JSON-RPC error returned by the plugin.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("plugin error %d: %s", e.Code, e.Message)
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// HostHandler dispatches host.* calls made by the plugin.
type HostHandler interface {
	Call(ctx context.Context, method string, params json.RawMessage) (any, error)
}

// Process is one running plugin speaking Content-Length framed JSON-RPC.
type Process struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	writeMu sync.Mutex
	nextID  atomic.Uint64

	mu      sync.Mutex
	pending map[string]chan *rpcMessage
	closed  bool
	err     error

	handler HostHandler
	done    chan struct{}
	exited  chan struct{}
	tail    *tailBuffer
}

// StartProcess launches command and starts the read loop.
func StartProcess(ctx context.Context, dir, command string, args, env []string, handler HostHandler) (*Process, error) {
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &Process{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		stderr:  stderr,
		pending: map[string]chan *rpcMessage{},
		handler: handler,
		done:    make(chan struct{}),
		exited:  make(chan struct{}),
		tail:    newTailBuffer(8 << 10),
	}
	go p.readLoop()
	go p.drainStderr()
	go func() {
		_ = cmd.Wait()
		close(p.exited)
		p.fail(errors.New("plugin exited"))
	}()
	return p, nil
}

// Call invokes one plugin method and decodes the result into out.
func (p *Process) Call(ctx context.Context, method string, params any, out any) error {
	raw, err := p.call(ctx, method, params)
	if err != nil {
		return err
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s: decode result: %w", method, err)
	}
	return nil
}

func (p *Process) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if err := p.waitReady(); err != nil {
		return nil, err
	}
	id := "h:" + strconv.FormatUint(p.nextID.Add(1), 10)
	msg := rpcMessage{JSONRPC: "2.0", ID: json.RawMessage(strconv.Quote(id)), Method: method}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		msg.Params = raw
	}
	ch := make(chan *rpcMessage, 1)
	p.mu.Lock()
	if p.closed {
		err := p.err
		p.mu.Unlock()
		if err == nil {
			err = errors.New("plugin closed")
		}
		return nil, err
	}
	p.pending[id] = ch
	p.mu.Unlock()

	if err := p.write(msg); err != nil {
		p.dropPending(id)
		return nil, err
	}

	timeout := callTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if left := time.Until(deadline); left < timeout {
			timeout = left
		}
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case reply := <-ch:
		if reply == nil {
			if err := p.failure(); err != nil {
				return nil, err
			}
			return nil, errors.New("plugin closed")
		}
		if reply.Error != nil {
			return nil, reply.Error
		}
		return reply.Result, nil
	case <-ctx.Done():
		p.dropPending(id)
		_ = p.Notify("$/cancelRequest", map[string]any{"id": id})
		return nil, ctx.Err()
	case <-timer.C:
		p.dropPending(id)
		_ = p.Notify("$/cancelRequest", map[string]any{"id": id})
		return nil, fmt.Errorf("%s: timed out after %s", method, timeout)
	}
}

// Notify sends a notification without waiting for a reply.
func (p *Process) Notify(method string, params any) error {
	msg := rpcMessage{JSONRPC: "2.0", Method: method}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return err
		}
		msg.Params = raw
	}
	return p.write(msg)
}

func (p *Process) write(msg rpcMessage) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if len(raw) > maxFrame {
		return fmt.Errorf("frame too large: %d", len(raw))
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(raw))
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	if _, err := io.WriteString(p.stdin, header); err != nil {
		return err
	}
	_, err = p.stdin.Write(raw)
	return err
}

func (p *Process) readLoop() {
	reader := bufio.NewReaderSize(p.stdout, 64<<10)
	for {
		payload, err := readFrame(reader)
		if err != nil {
			p.fail(err)
			return
		}
		var msg rpcMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			p.fail(fmt.Errorf("malformed frame: %w", err))
			return
		}
		p.dispatch(msg)
	}
}

func (p *Process) dispatch(msg rpcMessage) {
	switch {
	case msg.Method == "":
		// Response to one of our calls.
		var id string
		_ = json.Unmarshal(msg.ID, &id)
		p.mu.Lock()
		ch := p.pending[id]
		delete(p.pending, id)
		p.mu.Unlock()
		if ch != nil {
			ch <- &msg
		}
	case len(msg.ID) > 0:
		// Host service call: never block the read loop, the plugin may already be
		// waiting on a nested call while the host waits on this one.
		go p.serve(msg)
	default:
		p.notify(msg.Method, msg.Params)
	}
}

func (p *Process) serve(msg rpcMessage) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	var result any
	var err error
	if p.handler == nil {
		err = &RPCError{Code: -32601, Message: "host service unavailable"}
	} else {
		result, err = p.handler.Call(ctx, msg.Method, msg.Params)
	}
	reply := rpcMessage{JSONRPC: "2.0", ID: msg.ID}
	if err != nil {
		reply.Error = toRPCError(err)
	} else if raw, mErr := json.Marshal(result); mErr == nil {
		reply.Result = raw
	} else {
		reply.Error = &RPCError{Code: -32603, Message: mErr.Error()}
	}
	_ = p.write(reply)
}

// notify handles plugin notifications. Progress/cancel are acknowledged, the
// rest are ignored so a future notification never kills the process.
func (p *Process) notify(method string, params json.RawMessage) {
	switch method {
	case "initialized", "$/progress", "$/cancelRequest":
	default:
	}
}

func toRPCError(err error) *RPCError {
	var rpc *RPCError
	if errors.As(err, &rpc) {
		return rpc
	}
	return &RPCError{Code: -32603, Message: err.Error()}
}

func readFrame(reader *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("bad header %q", line)
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			n, convErr := strconv.Atoi(strings.TrimSpace(value))
			if convErr != nil || n < 0 {
				return nil, errors.New("bad Content-Length")
			}
			length = n
		}
	}
	if length < 0 {
		return nil, errors.New("missing Content-Length")
	}
	if length > maxFrame {
		return nil, fmt.Errorf("frame too large: %d", length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (p *Process) drainStderr() {
	_, _ = io.Copy(p.tail, p.stderr)
}

// Stderr returns the tail of the plugin's stderr for diagnostics.
func (p *Process) Stderr() string { return p.tail.String() }

func (p *Process) waitReady() error {
	select {
	case <-p.exited:
		if err := p.failure(); err != nil {
			return err
		}
		return errors.New("plugin exited")
	default:
		return nil
	}
}

func (p *Process) failure() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func (p *Process) dropPending(id string) {
	p.mu.Lock()
	delete(p.pending, id)
	p.mu.Unlock()
}

func (p *Process) fail(err error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.err = err
	chans := make([]chan *rpcMessage, 0, len(p.pending))
	for id, ch := range p.pending {
		delete(p.pending, id)
		chans = append(chans, ch)
	}
	p.mu.Unlock()
	for _, ch := range chans {
		select {
		case ch <- nil:
		default:
		}
	}
	close(p.done)
}

// Close asks the plugin to shut down, then kills it if it does not exit.
func (p *Process) Close() error {
	if p.cmd == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = p.Notify("shutdown", map[string]any{})
	_ = p.stdin.Close()
	select {
	case <-p.exited:
	case <-ctx.Done():
		_ = p.cmd.Process.Kill()
		select {
		case <-p.exited:
		case <-time.After(2 * time.Second):
		}
	}
	p.fail(errors.New("plugin closed"))
	return nil
}

type tailBuffer struct {
	mu   sync.Mutex
	max  int
	data []byte
}

func newTailBuffer(max int) *tailBuffer { return &tailBuffer{max: max} }

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data = append(t.data, p...)
	if len(t.data) > t.max {
		t.data = t.data[len(t.data)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.data)
}
