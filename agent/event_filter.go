package agent

// EventFilter is a sealed interface for OnEvent registration filters.
// Use AnyEvent / OnlyKinds / OnlyTool helpers; implement at your own risk.
type EventFilter interface {
	Match(ev Event) bool
}

type anyEventFilter struct{}

func (anyEventFilter) Match(Event) bool { return true }

// AnyEvent matches every Event.
var AnyEvent EventFilter = anyEventFilter{}

type kindFilter struct {
	kinds map[EventKind]struct{}
}

func (f kindFilter) Match(ev Event) bool {
	_, ok := f.kinds[ev.Kind]
	return ok
}

// OnlyKinds returns a filter that matches events whose Kind is in the given set.
func OnlyKinds(kinds ...EventKind) EventFilter {
	set := make(map[EventKind]struct{}, len(kinds))
	for _, k := range kinds {
		set[k] = struct{}{}
	}
	return kindFilter{kinds: set}
}

type toolFilter struct{ name string }

func (f toolFilter) Match(ev Event) bool {
	if ev.Tool == nil {
		return false
	}
	return ev.Tool.Name == f.name
}

// OnlyTool returns a filter that matches Pre/Post/ToolDelta events for the given tool.
func OnlyTool(name string) EventFilter {
	return toolFilter{name: name}
}
