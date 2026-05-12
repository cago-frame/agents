package agent_test

import (
	"testing"
	"time"

	agent "github.com/cago-frame/agents/agent"
)

func TestWatch_SingleSubscriber_Append(t *testing.T) {
	c := agent.NewConversation()
	seen := make(chan agent.Change, 4)
	go func() {
		for ch := range c.Watch() {
			seen <- ch
		}
	}()
	time.Sleep(10 * time.Millisecond) // Allow goroutine to enter the range loop
	c.Append(agent.Message{Role: agent.RoleUser})

	select {
	case ch := <-seen:
		if ch.Kind != agent.ChangeAppended {
			t.Fatalf("kind = %v", ch.Kind)
		}
		if ch.Index != 0 {
			t.Fatalf("index = %d", ch.Index)
		}
		if ch.Version <= 0 {
			t.Fatalf("version should be > 0, got %d", ch.Version)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for Change")
	}
}

func TestWatch_VersionMonotonic(t *testing.T) {
	c := agent.NewConversation()
	seen := make(chan agent.Change, 4)
	go func() {
		for ch := range c.Watch() {
			seen <- ch
		}
	}()
	time.Sleep(10 * time.Millisecond) // Allow goroutine to enter the range loop
	c.Append(agent.Message{Role: agent.RoleUser})
	c.Append(agent.Message{Role: agent.RoleAssistant})
	_ = c.Truncate(1)

	var versions []int64
	timeout := time.After(time.Second)
	for len(versions) < 3 {
		select {
		case ch := <-seen:
			versions = append(versions, ch.Version)
		case <-timeout:
			t.Fatalf("timed out, got versions=%v", versions)
		}
	}
	for i := 1; i < len(versions); i++ {
		if versions[i] <= versions[i-1] {
			t.Fatalf("versions not strictly increasing: %v", versions)
		}
	}
}

func TestWatch_MultipleSubscribers_GetSameChanges(t *testing.T) {
	c := agent.NewConversation()
	var sub1, sub2 []agent.Change
	done1, done2 := make(chan struct{}), make(chan struct{})

	go func() {
		defer close(done1)
		for ch := range c.Watch() {
			sub1 = append(sub1, ch)
			if len(sub1) == 2 {
				return
			}
		}
	}()
	go func() {
		defer close(done2)
		for ch := range c.Watch() {
			sub2 = append(sub2, ch)
			if len(sub2) == 2 {
				return
			}
		}
	}()
	// Give both goroutines a chance to register before we mutate.
	time.Sleep(20 * time.Millisecond)

	c.Append(agent.Message{Role: agent.RoleUser})
	c.Append(agent.Message{Role: agent.RoleAssistant})

	select {
	case <-done1:
	case <-time.After(time.Second):
		t.Fatalf("sub1 timed out, got %d changes", len(sub1))
	}
	select {
	case <-done2:
	case <-time.After(time.Second):
		t.Fatalf("sub2 timed out, got %d changes", len(sub2))
	}
	if len(sub1) != 2 || len(sub2) != 2 {
		t.Fatalf("each subscriber should see 2 changes, got %d / %d", len(sub1), len(sub2))
	}
	for i := range sub1 {
		if sub1[i].Version != sub2[i].Version {
			t.Fatalf("version mismatch at i=%d: %d vs %d", i, sub1[i].Version, sub2[i].Version)
		}
	}
}

func TestWatch_BreakUnsubscribes(t *testing.T) {
	c := agent.NewConversation()
	received := 0
	iter := c.Watch()
	go func() {
		for range iter {
			received++
			return // break out after first change
		}
	}()
	time.Sleep(10 * time.Millisecond)
	c.Append(agent.Message{Role: agent.RoleUser})
	time.Sleep(20 * time.Millisecond)
	c.Append(agent.Message{Role: agent.RoleAssistant})
	time.Sleep(20 * time.Millisecond)
	if received != 1 {
		t.Fatalf("subscriber should have stopped after break, got %d", received)
	}
}
