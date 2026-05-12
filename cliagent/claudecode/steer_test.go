package claudecode

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestWriteUserFrame_BasicContent(t *testing.T) {
	var buf bytes.Buffer
	if err := writeUserFrame(&buf, "hello world"); err != nil {
		t.Fatalf("writeUserFrame: %v", err)
	}
	s := buf.String()
	if !strings.Contains(s, `"hello world"`) {
		t.Errorf("frame missing text: %q", s)
	}
	if !strings.Contains(s, `"type":"user"`) {
		t.Errorf("frame missing type:user: %q", s)
	}
	// Must end with newline.
	if !strings.HasSuffix(s, "\n") {
		t.Errorf("frame missing trailing newline: %q", s)
	}
}

func TestWriteUserFrame_WriteError(t *testing.T) {
	ew := &errWriter{err: io.ErrClosedPipe}
	err := writeUserFrame(ew, "x")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// errWriter always returns the given error on Write.
type errWriter struct{ err error }

func (e *errWriter) Write(_ []byte) (int, error) { return 0, e.err }

func TestPermissionResponseFrameBytes_Allow(t *testing.T) {
	data, err := permissionResponseFrameBytes("perm-1", "allow")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `"decision":"allow"`) {
		t.Errorf("frame missing decision:allow: %q", s)
	}
	if !strings.Contains(s, `"id":"perm-1"`) {
		t.Errorf("frame missing id:perm-1: %q", s)
	}
	if !strings.HasSuffix(s, "\n") {
		t.Errorf("frame missing trailing newline: %q", s)
	}
}

func TestPermissionResponseFrameBytes_Deny(t *testing.T) {
	data, err := permissionResponseFrameBytes("perm-2", "deny")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `"decision":"deny"`) {
		t.Errorf("frame missing decision:deny: %q", s)
	}
}

func TestWritePermissionResponse_Allow(t *testing.T) {
	var buf bytes.Buffer
	if err := writePermissionResponse(&buf, "perm-3", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), `"decision":"allow"`) {
		t.Errorf("expected allow in output: %q", buf.String())
	}
}

func TestWritePermissionResponse_Deny(t *testing.T) {
	var buf bytes.Buffer
	if err := writePermissionResponse(&buf, "perm-4", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), `"decision":"deny"`) {
		t.Errorf("expected deny in output: %q", buf.String())
	}
}

func TestWritePermissionResponse_WriteError(t *testing.T) {
	ew := &errWriter{err: io.ErrClosedPipe}
	err := writePermissionResponse(ew, "perm-5", true)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
