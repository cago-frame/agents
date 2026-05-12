package coding

import (
	"testing"

	"github.com/cago-frame/agents/tool/bash"
	"github.com/cago-frame/agents/tool/edit"
	"github.com/cago-frame/agents/tool/find"
	"github.com/cago-frame/agents/tool/grep"
	"github.com/cago-frame/agents/tool/ls"
	"github.com/cago-frame/agents/tool/read"
	"github.com/cago-frame/agents/tool/todo"
	"github.com/cago-frame/agents/tool/webfetch"
	"github.com/cago-frame/agents/tool/websearch"
	"github.com/cago-frame/agents/tool/write"
)

type promptMetaProbe interface {
	PromptSnippet() string
	PromptGuidelines() []string
}

func TestBuiltinTools_AdvertisePromptMeta(t *testing.T) {
	jm := bash.NewJobManager()
	tools := map[string]any{
		"read":        read.New(read.Cwd(".")),
		"write":       write.New(write.Cwd(".")),
		"edit":        edit.New(edit.Cwd(".")),
		"bash":        bash.New(bash.Cwd("."), bash.Jobs(jm)),
		"bash_output": bash.NewOutput(jm),
		"kill_shell":  bash.NewKill(jm),
		"grep":        grep.New(grep.Cwd(".")),
		"find":        find.New(find.Cwd(".")),
		"ls":          ls.New(ls.Cwd(".")),
		"todo_write":  todo.New(),
		// websearch and webfetch implement PromptMeta; no provider required for metadata check.
		"web_search": websearch.New(),
		"web_fetch":  webfetch.New(),
	}
	for name, tl := range tools {
		pm, ok := tl.(promptMetaProbe)
		if !ok {
			t.Errorf("%s: does not implement PromptMeta", name)
			continue
		}
		if pm.PromptSnippet() == "" {
			t.Errorf("%s: empty PromptSnippet", name)
		}
		if len(pm.PromptGuidelines()) == 0 {
			t.Errorf("%s: empty PromptGuidelines", name)
		}
	}
}
