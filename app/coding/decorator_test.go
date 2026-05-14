package coding_test

import (
	"context"
	"testing"

	"github.com/cago-frame/agents/app/coding"
	"github.com/cago-frame/agents/provider/providertest"
	"github.com/cago-frame/agents/tool"
)

// TestSystem_WithToolDecorator_RenamesDefaultTools verifies that decorator
// returned from WithToolDecorator is invoked once per default parent tool
// and the rewritten name appears in Agent().Tools(). Custom extra tools are
// NOT decorated (they go through cfg.extraTools, not sess.All()).
func TestSystem_WithToolDecorator_RenamesDefaultTools(t *testing.T) {
	mock := providertest.New()
	seen := map[string]bool{}
	decorator := func(in tool.Tool) tool.Tool {
		seen[in.Name()] = true
		raw, ok := in.(*tool.RawTool)
		if !ok {
			return in
		}
		clone := *raw
		clone.NameStr = "x_" + raw.NameStr
		return &clone
	}

	sys, err := coding.New(t.Context(), mock, ".", coding.WithToolDecorator(decorator))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = sys.Close(context.Background()) })

	// Default Session.All() always returns bash/write/edit (and others).
	for _, want := range []string{"bash", "write", "edit"} {
		if !seen[want] {
			t.Errorf("decorator was not invoked for default tool %q (seen=%v)", want, keys(seen))
		}
	}

	names := map[string]bool{}
	for _, tt := range sys.Agent().Tools() {
		names[tt.Name()] = true
	}
	for _, want := range []string{"x_bash", "x_write", "x_edit"} {
		if !names[want] {
			t.Errorf("expected renamed tool %q in Agent().Tools(); got names=%v", want, keys(names))
		}
	}
	for _, gone := range []string{"bash", "write", "edit"} {
		if names[gone] {
			t.Errorf("original tool %q still present after rename (decorator did not replace)", gone)
		}
	}
}

// TestSystem_WithToolDecorator_NilReturnKeepsOriginal ensures returning the
// passed-in tool unchanged from the decorator is the no-op case (default behavior).
func TestSystem_WithToolDecorator_NilReturnKeepsOriginal(t *testing.T) {
	mock := providertest.New()
	decorator := func(in tool.Tool) tool.Tool { return in }

	sys, err := coding.New(t.Context(), mock, ".", coding.WithToolDecorator(decorator))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = sys.Close(context.Background()) })

	names := map[string]bool{}
	for _, tt := range sys.Agent().Tools() {
		names[tt.Name()] = true
	}
	for _, want := range []string{"bash", "write", "edit", "read"} {
		if !names[want] {
			t.Errorf("expected default tool %q still present when decorator is identity; got %v", want, keys(names))
		}
	}
}

// TestSystem_WithToolDecorator_DoesNotTouchExtraTools confirms that custom
// tools attached via WithExtraTools are NOT passed through the decorator —
// only Session.All() defaults are.
func TestSystem_WithToolDecorator_DoesNotTouchExtraTools(t *testing.T) {
	mock := providertest.New()
	extra := &tool.RawTool{NameStr: "my_remote_tool", DescStr: "remote"}

	var invokedFor []string
	decorator := func(in tool.Tool) tool.Tool {
		invokedFor = append(invokedFor, in.Name())
		return in
	}

	sys, err := coding.New(t.Context(), mock, ".",
		coding.WithExtraTools(extra),
		coding.WithToolDecorator(decorator),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = sys.Close(context.Background()) })

	for _, n := range invokedFor {
		if n == "my_remote_tool" {
			t.Errorf("decorator should not be called for WithExtraTools entries; was invoked for %q", n)
		}
	}

	// Extra tool must still be registered.
	found := false
	for _, tt := range sys.Agent().Tools() {
		if tt.Name() == "my_remote_tool" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("my_remote_tool missing from Agent().Tools() (invokedFor=%v)", invokedFor)
	}
}

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
