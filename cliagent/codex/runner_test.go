package codex_test

import (
	"context"
	"testing"

	"github.com/cago-frame/agents/cliagent/codex"
)

// TestRunner_NewBasicConstruction verifies Runner constructs cleanly with
// no options.
func TestRunner_NewBasicConstruction(t *testing.T) {
	r := codex.New()
	if r == nil {
		t.Fatal("New returned nil")
	}
	if err := r.Close(context.Background()); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestRunner_NewMixedOptions verifies Runner constructs with backend opts
// (Cwd, Model) in one call.
func TestRunner_NewMixedOptions(t *testing.T) {
	r := codex.New(
		codex.Cwd(t.TempDir()),
		codex.Model("gpt-5"),
	)
	if r == nil {
		t.Fatal("New returned nil")
	}
	_ = r.Close(context.Background())
}

// TestRunner_SessionDelegates verifies Runner.Session returns a usable
// *codex.Session.
func TestRunner_SessionDelegates(t *testing.T) {
	r := codex.New()
	defer r.Close(context.Background()) //nolint:errcheck
	sess := r.Session()
	if sess == nil {
		t.Fatal("Session returned nil")
	}
}

// TestRunner_CloseIdempotent verifies Close is safe to call multiple times.
func TestRunner_CloseIdempotent(t *testing.T) {
	r := codex.New()
	if err := r.Close(context.Background()); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := r.Close(context.Background()); err != nil {
		t.Errorf("second Close: %v", err)
	}
}
