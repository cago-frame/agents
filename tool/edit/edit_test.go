package edit_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
	"github.com/cago-frame/agents/tool/edit"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func callTool(t *testing.T, tl tool.Tool, args map[string]any) (*agent.ToolResultBlock, error) {
	t.Helper()
	return tl.Call(context.Background(), args)
}

func TestEditSingleReplacement(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "a.txt", "alpha\nbeta\ngamma")
	tl := edit.New(edit.Cwd(dir))

	out, err := callTool(t, tl, map[string]any{
		"path": "a.txt",
		"edits": []map[string]any{
			{"oldText": "beta", "newText": "BETA"},
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected error result: %s", out.Content[0].(agent.TextBlock).Text)
	}
	text := out.Content[0].(agent.TextBlock).Text
	if !strings.Contains(text, "Successfully replaced 1 block(s)") {
		t.Fatalf("unexpected: %q", text)
	}
	got, _ := os.ReadFile(p) //nolint:gosec // test reads back files we just wrote
	if string(got) != "alpha\nBETA\ngamma" {
		t.Fatalf("file: %q", got)
	}
}

func TestEditMultiReplacements(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "a.txt", "one\ntwo\nthree")
	tl := edit.New(edit.Cwd(dir))

	out, err := callTool(t, tl, map[string]any{
		"path": "a.txt",
		"edits": []map[string]any{
			{"oldText": "one", "newText": "1"},
			{"oldText": "three", "newText": "3"},
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected error result: %s", out.Content[0].(agent.TextBlock).Text)
	}
	got, _ := os.ReadFile(p) //nolint:gosec // test reads back files we just wrote
	if string(got) != "1\ntwo\n3" {
		t.Fatalf("file: %q", got)
	}
}

func TestEditNotFound(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "alpha")
	tl := edit.New(edit.Cwd(dir))
	out, err := callTool(t, tl, map[string]any{
		"path":  "a.txt",
		"edits": []map[string]any{{"oldText": "missing", "newText": "x"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.IsError {
		t.Fatal("expected error result")
	}
	msg := out.Content[0].(agent.TextBlock).Text
	if !strings.Contains(msg, "Could not find the exact text in a.txt") {
		t.Fatalf("wrong err: %q", msg)
	}
}

func TestEditDuplicate(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "x\nx\nx")
	tl := edit.New(edit.Cwd(dir))
	out, err := callTool(t, tl, map[string]any{
		"path":  "a.txt",
		"edits": []map[string]any{{"oldText": "x", "newText": "y"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.IsError {
		t.Fatal("expected error result")
	}
	msg := out.Content[0].(agent.TextBlock).Text
	if !strings.Contains(msg, "Found 3 occurrences of the text") {
		t.Fatalf("wrong err: %q", msg)
	}
}

func TestEditOverlapRejected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "abcdef")
	tl := edit.New(edit.Cwd(dir))
	out, err := callTool(t, tl, map[string]any{
		"path": "a.txt",
		"edits": []map[string]any{
			{"oldText": "abcd", "newText": "X"},
			{"oldText": "cdef", "newText": "Y"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.IsError {
		t.Fatal("expected error result")
	}
	msg := out.Content[0].(agent.TextBlock).Text
	if !strings.Contains(msg, "overlap") {
		t.Fatalf("wrong err: %q", msg)
	}
}

func TestEditEmptyOldText(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "x")
	tl := edit.New(edit.Cwd(dir))
	out, err := callTool(t, tl, map[string]any{
		"path":  "a.txt",
		"edits": []map[string]any{{"oldText": "", "newText": "y"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.IsError {
		t.Fatal("expected error result")
	}
	msg := out.Content[0].(agent.TextBlock).Text
	if !strings.Contains(msg, "must not be empty") {
		t.Fatalf("wrong err: %q", msg)
	}
}

func TestEditNoChange(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "alpha")
	tl := edit.New(edit.Cwd(dir))
	out, err := callTool(t, tl, map[string]any{
		"path":  "a.txt",
		"edits": []map[string]any{{"oldText": "alpha", "newText": "alpha"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.IsError {
		t.Fatal("expected error result")
	}
	msg := out.Content[0].(agent.TextBlock).Text
	if !strings.Contains(msg, "No changes made") {
		t.Fatalf("wrong err: %q", msg)
	}
}

func TestEditFuzzySmartQuotes(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "a.txt", "alpha 'quoted' beta")
	tl := edit.New(edit.Cwd(dir))
	out, err := callTool(t, tl, map[string]any{
		"path":  "a.txt",
		"edits": []map[string]any{{"oldText": "‘quoted’", "newText": "Q"}},
	})
	if err != nil {
		t.Fatalf("fuzzy match should succeed, got %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected error result: %s", out.Content[0].(agent.TextBlock).Text)
	}
	got, _ := os.ReadFile(p) //nolint:gosec // test reads back files we just wrote
	if string(got) != "alpha Q beta" {
		t.Fatalf("file: %q", got)
	}
}

func TestEditPreservesCRLF(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "a.txt", "one\r\ntwo\r\nthree")
	tl := edit.New(edit.Cwd(dir))
	out, err := callTool(t, tl, map[string]any{
		"path":  "a.txt",
		"edits": []map[string]any{{"oldText": "two", "newText": "TWO"}},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected error result: %s", out.Content[0].(agent.TextBlock).Text)
	}
	got, _ := os.ReadFile(p) //nolint:gosec // test reads back files we just wrote
	if string(got) != "one\r\nTWO\r\nthree" {
		t.Fatalf("file CRLF lost: %q", got)
	}
}

func TestEditLegacyTopLevelOldNew(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "a.txt", "alpha")
	tl := edit.New(edit.Cwd(dir))
	out, err := tl.Call(context.Background(), map[string]any{
		"path": "a.txt", "oldText": "alpha", "newText": "ALPHA",
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected error result: %s", out.Content[0].(agent.TextBlock).Text)
	}
	got, _ := os.ReadFile(p) //nolint:gosec // test reads back files we just wrote
	if string(got) != "ALPHA" {
		t.Fatalf("file: %q", got)
	}
}

func TestEditFileNotFound(t *testing.T) {
	dir := t.TempDir()
	tl := edit.New(edit.Cwd(dir))
	out, err := callTool(t, tl, map[string]any{
		"path":  "nope.txt",
		"edits": []map[string]any{{"oldText": "a", "newText": "b"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.IsError {
		t.Fatal("expected error result")
	}
	msg := out.Content[0].(agent.TextBlock).Text
	if !strings.Contains(msg, "Could not edit file") {
		t.Fatalf("wrong err: %q", msg)
	}
}
