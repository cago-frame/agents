package state_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cago-frame/agents/tool/state"
)

func TestCheckNilTrackerYieldsErrTrackerNil(t *testing.T) {
	var tr *state.ReadTracker
	_, err := tr.Check("/tmp/x")
	if !errors.Is(err, state.ErrTrackerNil) {
		t.Fatalf("got %v", err)
	}
}

func TestCheckUnreadFileErrNotRead(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	tr := state.NewReadTracker()
	_, err := tr.Check(p)
	if !errors.Is(err, state.ErrNotRead) {
		t.Fatalf("expected ErrNotRead, got %v", err)
	}
}

func TestCheckRecordedFileOk(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(p, []byte("hello"), 0o600)
	st, _ := os.Stat(p)
	tr := state.NewReadTracker()
	tr.Record(p, st)
	if _, err := tr.Check(p); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
}

func TestCheckStaleAfterModification(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(p, []byte("hello"), 0o600)
	st, _ := os.Stat(p)
	tr := state.NewReadTracker()
	tr.Record(p, st)

	// bump mtime so the check trips ErrStale
	future := time.Now().Add(time.Hour)
	_ = os.Chtimes(p, future, future)

	_, err := tr.Check(p)
	if !errors.Is(err, state.ErrStale) {
		t.Fatalf("expected ErrStale, got %v", err)
	}
}

func TestCheckMissingFileSkippedForWrite(t *testing.T) {
	tr := state.NewReadTracker()
	if _, err := tr.Check("/tmp/__definitely_not_here_xyz__"); err != nil {
		t.Fatalf("missing file should be ok (write may create), got %v", err)
	}
}

func TestForgetRemovesRecord(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(p, []byte("x"), 0o600)
	st, _ := os.Stat(p)
	tr := state.NewReadTracker()
	tr.Record(p, st)
	tr.Forget(p)
	_, err := tr.Check(p)
	if !errors.Is(err, state.ErrNotRead) {
		t.Fatalf("expected ErrNotRead after Forget, got %v", err)
	}
}
