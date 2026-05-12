package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

// fakeRunner is a fake appServerRunner used by codex tests. Each Start call
// invokes handler in a fresh goroutine with the corresponding fakeAppHandle.
type fakeRunner struct {
	mu      sync.Mutex
	opts    []procOptions
	handler func(*testing.T, *fakeAppHandle)
	t       *testing.T
}

func (r *fakeRunner) Start(ctx context.Context, opts procOptions) (processHandle, error) {
	_ = ctx
	r.mu.Lock()
	r.opts = append(r.opts, opts)
	r.mu.Unlock()
	h := newFakeAppHandle()
	go r.handler(r.t, h)
	return h, nil
}

// fakeAppHandle plays the role of a codex app-server child process.
type fakeAppHandle struct {
	stdinR   *io.PipeReader
	stdinW   *io.PipeWriter
	stdoutR  *io.PipeReader
	stdoutW  *io.PipeWriter
	stderrR  *strings.Reader
	done     chan struct{}
	doneOnce sync.Once
}

func newFakeAppHandle() *fakeAppHandle {
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	return &fakeAppHandle{
		stdinR:  stdinR,
		stdinW:  stdinW,
		stdoutR: stdoutR,
		stdoutW: stdoutW,
		stderrR: strings.NewReader(""),
		done:    make(chan struct{}),
	}
}

func (h *fakeAppHandle) Stdin() io.Writer  { return h.stdinW }
func (h *fakeAppHandle) Stdout() io.Reader { return h.stdoutR }
func (h *fakeAppHandle) Stderr() io.Reader { return h.stderrR }
func (h *fakeAppHandle) Wait() error {
	<-h.done
	return nil
}
func (h *fakeAppHandle) Kill() error {
	_ = h.stdinW.Close()
	_ = h.stdoutW.Close()
	h.doneOnce.Do(func() { close(h.done) })
	return nil
}
func (h *fakeAppHandle) Signal(sig os.Signal) error {
	_ = sig
	return h.Kill()
}

func (h *fakeAppHandle) send(v any) {
	data, _ := json.Marshal(v)
	_, _ = h.stdoutW.Write(append(data, '\n'))
}

type rpcReq struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
}

func readReq(t *testing.T, sc *bufio.Scanner) rpcReq {
	t.Helper()
	if !sc.Scan() {
		t.Fatalf("server stdin closed: %v", sc.Err())
	}
	var req rpcReq
	if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return req
}

func respond(h *fakeAppHandle, req rpcReq, result any) {
	h.send(map[string]any{"id": json.RawMessage(req.ID), "result": result})
}
