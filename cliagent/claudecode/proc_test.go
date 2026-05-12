package claudecode

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestExecRunner_EchoCommand(t *testing.T) {
	r := &execRunner{}
	p, err := r.Start(context.Background(), procOptions{
		Binary: "sh",
		Args:   []string{"-c", "echo hello; echo world 1>&2"},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	stdout, _ := io.ReadAll(p.Stdout())
	stderr, _ := io.ReadAll(p.Stderr())
	if err := p.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if !strings.Contains(string(stdout), "hello") {
		t.Fatalf("stdout: %q", string(stdout))
	}
	if !strings.Contains(string(stderr), "world") {
		t.Fatalf("stderr: %q", string(stderr))
	}
}

func TestExecRunner_KillStopsLongProcess(t *testing.T) {
	r := &execRunner{}
	p, err := r.Start(context.Background(), procOptions{
		Binary: "sh",
		Args:   []string{"-c", "sleep 30"},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := p.Kill(); err != nil {
		t.Fatalf("kill: %v", err)
	}
	_ = p.Wait() // returns non-nil, acceptable
}
